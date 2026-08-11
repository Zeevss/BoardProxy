package nodeconfig

import (
	"errors"
	"flag"
	"os"
	"strings"
	"time"
)

const (
	defaultCollectInterval   = 10 * time.Second
	defaultHeartbeatInterval = 15 * time.Second
)

type Config struct {
	DataDirectory   string
	BootstrapSecret string
	CoreBinary      string
	CoreControl     string
	Interfaces      []string
	SysClassNet     string
	CollectInterval time.Duration
	Heartbeat       time.Duration
}

func Parse(args []string) (Config, error) {
	flags := flag.NewFlagSet("bproxy-node", flag.ContinueOnError)
	data := flags.String("data", env("BPROXY_NODE_DATA", "/var/lib/bproxy-node"), "persistent node-agent data directory")
	secret := flags.String("secret", os.Getenv("BPROXY_NODE_SECRET"), "base64 enrollment secret from control-plane")
	coreBinary := flags.String("core-binary", env("BPROXY_CORE_BINARY", "/usr/local/bin/bproxy"), "core binary path")
	coreControl := flags.String("core-control", env("BPROXY_CORE_CONTROL", "unix:///run/bproxy/control.sock"), "local core gRPC address")
	interfaces := flags.String("interfaces", env("BPROXY_STATS_INTERFACES", "eth0"), "comma-separated container interfaces")
	sysClassNet := flags.String("sys-class-net", env("BPROXY_SYS_CLASS_NET", "/sys/class/net"), "network counter root")
	collectInterval := flags.Duration("collect-interval", defaultCollectInterval, "traffic collection interval")
	heartbeat := flags.Duration("heartbeat-interval", defaultHeartbeatInterval, "hub heartbeat interval")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	config := Config{
		DataDirectory: *data, BootstrapSecret: *secret, CoreBinary: *coreBinary, CoreControl: *coreControl,
		Interfaces: split(*interfaces), SysClassNet: *sysClassNet,
		CollectInterval: *collectInterval, Heartbeat: *heartbeat,
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.DataDirectory == "" || c.CoreBinary == "" || c.CoreControl == "" {
		return errors.New("data directory, core binary and core control address are required")
	}
	if c.CollectInterval <= 0 || c.Heartbeat <= 0 {
		return errors.New("collection and heartbeat intervals must be positive")
	}
	if len(c.Interfaces) == 0 {
		return errors.New("at least one statistics interface is required")
	}
	return nil
}

func split(raw string) []string {
	var values []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
