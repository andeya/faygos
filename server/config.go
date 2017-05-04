package server

import (
	"path"
	"strings"

	"github.com/henrylee2cn/faygo"
)

type Config struct {
	BasePath           string   `ini:"base_path"`
	EtcdUrls           []string `ini:"etcd_urls" delim:"|"`
	EtcdDialTimeout    uint32   `ini:"etcd_dial_timeout"`    // second
	EtcdUpdateInterval uint32   `ini:"etcd_update_interval"` // second
	faygo.Config       `ini:"DEFAULT"`
}

func newConfig(filename string) (Config, error) {
	structPointer := &Config{
		EtcdDialTimeout:    10,
		EtcdUpdateInterval: 30,
		EtcdUrls:           []string{"http://127.0.0.1:2379"},
	}

	return *structPointer, faygo.SyncINI(
		structPointer,
		func(existed bool, saveOnce func() error) error {
			if existed && structPointer.BasePath == "" {
				structPointer.BasePath = "/faygos"
				return saveOnce()
			}
			structPointer.BasePath = strings.TrimRight(path.Join("/", structPointer.BasePath), "/")
			return nil
		},
		filename,
	)
}
