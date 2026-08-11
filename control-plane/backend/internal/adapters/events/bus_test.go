package events

import (
	"testing"
	"time"
)

func TestBusNotifiesOnlyMatchingSubscribersAndCoalesces(t *testing.T) {
	bus := New()
	nodeOne, unsubscribe := bus.Subscribe("node-1")
	defer unsubscribe()
	nodeTwo, unsubscribeTwo := bus.Subscribe("node-2")
	defer unsubscribeTwo()
	bus.Notify("node-1")
	bus.Notify("node-1")
	select {
	case <-nodeOne:
	case <-time.After(time.Second):
		t.Fatal("node-1 was not notified")
	}
	select {
	case <-nodeOne:
		t.Fatal("duplicate wake-up was not coalesced")
	default:
	}
	select {
	case <-nodeTwo:
		t.Fatal("notification leaked to another node")
	default:
	}
}

func TestUnsubscribeStopsNotifications(t *testing.T) {
	bus := New()
	channel, unsubscribe := bus.Subscribe("node-1")
	unsubscribe()
	unsubscribe()
	bus.Notify("node-1")
	select {
	case <-channel:
		t.Fatal("unsubscribed channel was notified")
	default:
	}
}
