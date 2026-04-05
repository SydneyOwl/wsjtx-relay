package relay

import (
	"testing"

	relaypb "github.com/SydneyOwl/wsjtx-relay-proto/gen/go/v20260405"
)

func TestLiveEventDispatcherDropsWhenInactive(t *testing.T) {
	dispatcher := &liveEventDispatcher{}

	delivered, connected := dispatcher.publish(&relaypb.Envelope{})
	if delivered {
		t.Fatal("expected inactive dispatcher to drop the event")
	}
	if connected {
		t.Fatal("expected inactive dispatcher to report no active connection")
	}
}

func TestLiveEventDispatcherDeliversToActiveChannel(t *testing.T) {
	dispatcher := &liveEventDispatcher{}
	events := make(chan *relaypb.Envelope, 1)
	envelope := &relaypb.Envelope{}

	dispatcher.activate(events)
	delivered, connected := dispatcher.publish(envelope)
	if !connected {
		t.Fatal("expected active dispatcher to report a live connection")
	}
	if !delivered {
		t.Fatal("expected active dispatcher to deliver the event")
	}

	select {
	case got := <-events:
		if got != envelope {
			t.Fatal("dispatcher delivered an unexpected envelope instance")
		}
	default:
		t.Fatal("expected the active channel to receive the envelope")
	}
}

func TestLiveEventDispatcherReportsBackpressure(t *testing.T) {
	dispatcher := &liveEventDispatcher{}
	events := make(chan *relaypb.Envelope, 1)

	dispatcher.activate(events)
	events <- &relaypb.Envelope{}

	delivered, connected := dispatcher.publish(&relaypb.Envelope{})
	if !connected {
		t.Fatal("expected dispatcher to report an active connection")
	}
	if delivered {
		t.Fatal("expected dispatcher to reject the event when the queue is full")
	}
}

func TestLiveEventDispatcherDeactivateOnlyClearsMatchingChannel(t *testing.T) {
	dispatcher := &liveEventDispatcher{}
	oldEvents := make(chan *relaypb.Envelope, 1)
	newEvents := make(chan *relaypb.Envelope, 1)

	dispatcher.activate(oldEvents)
	dispatcher.activate(newEvents)
	dispatcher.deactivate(oldEvents)

	delivered, connected := dispatcher.publish(&relaypb.Envelope{})
	if !connected || !delivered {
		t.Fatal("expected stale deactivation to leave the newer active channel intact")
	}
}
