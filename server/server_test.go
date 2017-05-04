package server

import (
	"testing"
)

func TestNew(t *testing.T) {
	srv, err := New("testserver", "1.0")
	t.Logf("config:%#v\nerror:%v", srv.config, err)
}
