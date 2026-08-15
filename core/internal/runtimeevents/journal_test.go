package runtimeevents

import "testing"

func TestJournalReplaysAndReportsGap(t *testing.T) {
	journal := New(2)
	first := journal.Publish(Event{Type: ResourceChanged, Tag: "one"})
	journal.Publish(Event{Type: ResourceChanged, Tag: "two"})
	third := journal.Publish(Event{Type: ResourceChanged, Tag: "three"})

	subscription := journal.Subscribe(first.BootID, 0)
	defer subscription.Close()
	if subscription.Reset == nil || subscription.Reset.Reason != "event_gap" {
		t.Fatalf("reset=%+v, want event_gap", subscription.Reset)
	}
	if len(subscription.Replay) != 2 || subscription.Replay[1].Sequence != third.Sequence {
		t.Fatalf("replay=%+v", subscription.Replay)
	}
}

func TestJournalBootMismatchIsExplicit(t *testing.T) {
	journal := New(4)
	journal.Publish(Event{Type: ResourceChanged})
	subscription := journal.Subscribe("previous-boot", 8)
	defer subscription.Close()
	if subscription.Reset == nil || subscription.Reset.Reason != "core_restarted" {
		t.Fatalf("reset=%+v", subscription.Reset)
	}
}
