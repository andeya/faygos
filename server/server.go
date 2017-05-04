package server

import (
	"net"
	"time"

	"github.com/henrylee2cn/faygo"
	"github.com/rcrowley/go-metrics"
)

type Server struct {
	*faygo.Framework
	etcdV3Registers []*EtcdV3Register
	config          Config
}

// New creates a new Server
func New(name string, version ...string) (*Server, error) {
	frame := faygo.New(name, version...)
	config, err := newConfig(frame.ConfigFilename())
	if err != nil {
		return nil, err
	}
	etcdV3Registers := make([]*EtcdV3Register, len(config.Addrs))
	for i := range etcdV3Registers {
		_, port, _ := net.SplitHostPort(config.Addrs[i])
		etcdV3Registers[i] = &EtcdV3Register{
			Port:            port,
			EtcdServers:     config.EtcdUrls,
			BasePath:        config.BasePath,
			Metrics:         metrics.NewRegistry(),
			DialEtcdTimeout: time.Duration(config.EtcdDialTimeout) * time.Second,
			UpdateInterval:  time.Duration(config.EtcdUpdateInterval) * time.Second,
		}
	}
	return &Server{
		Framework:       frame,
		etcdV3Registers: etcdV3Registers,
		config:          config,
	}, nil
}

// Run starts all web services.
func (s *Server) Run(metadata ...string) {
	for _, reg := range s.etcdV3Registers {
		if err := reg.Start(); err != nil {
			s.Log().Fatal(err)
		}
		for _, mux := range s.MuxAPIsForRouter() {
			if err := reg.Register(mux.Path(), metadata...); err != nil {
				s.Log().Fatal(err)
			}
		}
	}
	s.Framework.Run()
}
