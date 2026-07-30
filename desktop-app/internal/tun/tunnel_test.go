package tun

import (
	"errors"
	"testing"

	"bproxy-core/pkg/bproxy"
)

// fakePlatform фиксирует порядок вызовов и умеет имитировать ошибку настройки.
type fakePlatform struct {
	applyErr    error
	revertErr   error
	events      []string
	appliedName string
}

func (f *fakePlatform) protector() bproxy.SocketProtector { return nil }

func (f *fakePlatform) applyRouting(ifName string, _ Params) error {
	f.events = append(f.events, "apply")
	f.appliedName = ifName
	return f.applyErr
}

func (f *fakePlatform) revertRouting() error {
	f.events = append(f.events, "revert")
	return f.revertErr
}

type fakeEngine struct{ stopped *bool }

func (e fakeEngine) stop() { *e.stopped = true }

func newTestController(plat platform, startErr error, ifName string, engStopped *bool) *Controller {
	return &Controller{
		plat: plat,
		startFn: func(_, _ string, _ int) (stackEngine, string, error) {
			if startErr != nil {
				return nil, "", startErr
			}
			return fakeEngine{stopped: engStopped}, ifName, nil
		},
	}
}

func TestStartSetsUpEngineThenRouting(t *testing.T) {
	plat := &fakePlatform{}
	var engStopped bool
	c := newTestController(plat, nil, "utun9", &engStopped)

	if err := c.Start(Params{ProxyAddr: "127.0.0.1:1080"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if plat.appliedName != "utun9" {
		t.Fatalf("routing applied to %q, want utun9", plat.appliedName)
	}
	if got := len(plat.events); got != 1 || plat.events[0] != "apply" {
		t.Fatalf("events = %v, want [apply]", plat.events)
	}

	if err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !engStopped {
		t.Fatal("engine was not stopped")
	}
	if len(plat.events) != 2 || plat.events[1] != "revert" {
		t.Fatalf("events = %v, want [apply revert]", plat.events)
	}
}

func TestStartRollsBackEngineOnRoutingFailure(t *testing.T) {
	routeErr := errors.New("no permission")
	plat := &fakePlatform{applyErr: routeErr}
	var engStopped bool
	c := newTestController(plat, nil, "utun9", &engStopped)

	if err := c.Start(Params{ProxyAddr: "127.0.0.1:1080"}); !errors.Is(err, routeErr) {
		t.Fatalf("Start err = %v, want routeErr", err)
	}
	if !engStopped {
		t.Fatal("engine must be stopped when routing setup fails")
	}
	if c.started {
		t.Fatal("controller must not be marked started after failure")
	}
}

func TestStartRequiresProxyAddr(t *testing.T) {
	c := newTestController(&fakePlatform{}, nil, "utun9", new(bool))
	if err := c.Start(Params{}); err == nil {
		t.Fatal("expected error for empty ProxyAddr")
	}
}

func TestStopIsIdempotentAndSafeWithoutStart(t *testing.T) {
	plat := &fakePlatform{}
	c := newTestController(plat, nil, "utun9", new(bool))

	// Stop без Start не должен трогать маршруты и не паниковать.
	if err := c.Stop(); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	if len(plat.events) != 0 {
		t.Fatalf("revert called without Start: %v", plat.events)
	}
	// Повторный Stop безопасен.
	if err := c.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestEngineStartFailurePropagates(t *testing.T) {
	startErr := errors.New("device busy")
	plat := &fakePlatform{}
	c := newTestController(plat, startErr, "", new(bool))
	if err := c.Start(Params{ProxyAddr: "127.0.0.1:1080"}); !errors.Is(err, startErr) {
		t.Fatalf("Start err = %v, want startErr", err)
	}
	if len(plat.events) != 0 {
		t.Fatalf("routing must not run when engine fails: %v", plat.events)
	}
}
