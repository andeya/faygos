package server

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/henrylee2cn/currip"
	"github.com/henrylee2cn/faygo"
	"github.com/rcrowley/go-metrics"
	"golang.org/x/net/context"
)

//EtcdV3Register a register plugin which can register services into etcd for cluster
type EtcdV3Register struct {
	Port            string
	EtcdServers     []string
	BasePath        string
	Metrics         metrics.Registry
	services        []string
	UpdateInterval  time.Duration
	KeysAPI         *clientv3.Client
	ticker          *time.Ticker
	DialEtcdTimeout time.Duration
	Username        string
	Password        string
	metadata        url.Values
	metadataRWMutex sync.RWMutex
	ttlOp           clientv3.OpOption
	serviceAddress  string
}

// Start starts to connect etcd v3 cluster
func (p *EtcdV3Register) Start() error {
	extranetHost, err := currip.ExtranetIP()
	if err != nil {
		return err
	}
	intranetHost, err := currip.IntranetIP()
	if err != nil {
		return err
	}
	if p.Port == "" {
		p.Port = "80"
	}
	p.serviceAddress = net.JoinHostPort(extranetHost, p.Port) + "@" + net.JoinHostPort(intranetHost, p.Port)
	for {
		p.KeysAPI, err = clientv3.New(clientv3.Config{
			Endpoints:   p.EtcdServers,
			DialTimeout: p.DialEtcdTimeout,
			Username:    p.Username,
			Password:    p.Password,
		})
		if err == nil {
			faygo.Infof("new etcd client...")
			break
		}
		faygo.Warningf("try to new etcd client: %s", err.Error())
		time.Sleep(time.Minute)
	}
	return p.setGrantKeepAlive()
}

// Register handles registering event.
// this service is registered at BASE/serviceName/thisIpAddress node
func (p *EtcdV3Register) Register(name string, metadata ...string) error {
	if "" == p.serviceAddress {
		return errors.New("EtcdV3Register not yet started!")
	}
	if "" == strings.TrimSpace(name) {
		return errors.New("service `name` can't be empty!")
	}

	var err error
	var nodePath = path.Join(p.BasePath, name, p.serviceAddress)

	p.metadata, err = url.ParseQuery(strings.Join(metadata, "&"))
	if err != nil {
		return errors.New("metadata must be query format")
	}
	if p.ttlOp != nil {
		err = p.Put(nodePath, p.metadata.Encode(), p.ttlOp)
	} else {
		err = p.Put(nodePath, p.metadata.Encode())
	}
	if err != nil {
		return errors.New("failed to put service meta: " + err.Error())
	}

	if !isContainsV3(p.services, nodePath) {
		p.services = append(p.services, nodePath)
	}
	return nil
}

//Close closes this plugin
func (p *EtcdV3Register) Close() {
	p.ticker.Stop()
}

//SetMetadata sets meta data
func (p *EtcdV3Register) SetMetadata(metadata string) (err error) {
	p.metadataRWMutex.Lock()
	p.metadata, err = url.ParseQuery(metadata)
	p.metadataRWMutex.Unlock()
	if err != nil {
		return err
	}
	var errs []error
	p.metadataRWMutex.RLock()
	defer p.metadataRWMutex.RUnlock()
	var (
		resp *clientv3.GetResponse
		v    url.Values
	)
	for _, nodePath := range p.services {
		if p.DialEtcdTimeout > 0 {
			ctx, _ := context.WithTimeout(context.Background(), p.DialEtcdTimeout)
			resp, err = p.KeysAPI.Get(ctx, nodePath)
		} else {
			resp, err = p.KeysAPI.Get(context.TODO(), nodePath)
		}
		if err == nil {
			if len(resp.Kvs) == 0 {
				v = url.Values{}
			} else if v, err = url.ParseQuery(string(resp.Kvs[0].Value)); err == nil {
				// reset metadata
				p.metadataRWMutex.RLock()
				for k := range p.metadata {
					v.Set(k, p.metadata.Get(k))
				}
				p.metadataRWMutex.RUnlock()
				if p.ttlOp != nil {
					if err = p.Put(nodePath, v.Encode(), p.ttlOp); err != nil {
						if strings.Contains(err.Error(), "requested lease not found") {
							if err = p.setGrantKeepAlive(); err != nil {
								faygo.Errorf("%s", err.Error())
							} else {
								err = p.Put(nodePath, v.Encode(), p.ttlOp)
							}
						} else {
							faygo.Errorf("%s", err.Error())
						}
					}
				} else {
					if err = p.Put(nodePath, v.Encode()); err != nil {
						faygo.Errorf("%s", err.Error())
					}
				}
			} else {
				errs = append(errs, err)
			}
		} else {
			errs = append(errs, err)
		}
	}
	return faygo.Errors(errs)
}

//Put KV by V3 API
func (p *EtcdV3Register) Put(key, value string, opts ...clientv3.OpOption) error {
	_, err := p.KeysAPI.Put(context.TODO(), key, value, opts...)
	if err != nil {
		return fmt.Errorf("put: %s %s, error %s", key, value, err.Error())
	}
	return nil
}

func (p *EtcdV3Register) setGrantKeepAlive() error {
	//TTL
	resp, err := p.KeysAPI.Grant(context.TODO(), int64(p.UpdateInterval/time.Second)+5)
	if err != nil {
		return fmt.Errorf("etct_v3 TTL Grant: %s", err.Error())
	}
	//KeepAlive TTL alive forever
	ch, err := p.KeysAPI.KeepAlive(context.TODO(), resp.ID)
	ka := <-ch
	if err != nil || ka == nil {
		return fmt.Errorf("set: etct_v3 TTL Keepalive is forver, error: %v", err)
	}
	faygo.Debugf("set: etct_v3 TTL is %d second", ka.TTL)
	p.ttlOp = clientv3.WithLease(resp.ID)
	return nil
}

func isContainsV3(list []string, element string) (exist bool) {
	exist = false
	if list == nil || len(list) <= 0 {
		return
	}
	for index := 0; index < len(list); index++ {
		if list[index] == element {
			return true
		}
	}
	return
}

// Unregister a service from etcd but this service still exists in this node.
func (p *EtcdV3Register) Unregister(name string) (err error) {
	if "" == strings.TrimSpace(name) {
		err = errors.New("unregister service name cann't be empty!")
		return
	}
	nodePath := path.Join(p.BasePath, name, p.serviceAddress)

	_, err = p.KeysAPI.Delete(context.TODO(), nodePath, clientv3.WithPrefix())
	if err != nil {
		return
	}
	// because plugin.Start() method will be executed by timer continuously
	// so it need to remove the service nodePath from service list
	if p.services == nil || len(p.services) <= 0 {
		return nil
	}
	var index int
	for index = 0; index < len(p.services); index++ {
		if p.services[index] == nodePath {
			break
		}
	}
	if index != len(p.services) {
		p.services = append(p.services[:index], p.services[index+1:]...)
	}

	return
}
