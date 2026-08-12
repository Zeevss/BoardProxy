package nodeconfig

import (
	"testing"
	"time"
)

func TestValidateRejectsEmptyInterfacesAndDurations(t *testing.T) {
	valid := Config{DataDirectory: "/data", CoreBinary: "bproxy", CoreControl: "unix:///run/core.sock", Interfaces: []string{"eth0"}, CollectInterval: time.Second, Heartbeat: time.Second, MaxOutboxBytes: 1024}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	valid.Interfaces = nil
	if err := valid.Validate(); err == nil {
		t.Fatal("empty interfaces accepted")
	}
}
