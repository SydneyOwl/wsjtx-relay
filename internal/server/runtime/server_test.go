package runtime

import (
	"testing"

	relaypb "github.com/SydneyOwl/wsjtx-relay-proto/gen/go/v20260405"
)

func TestSequenceWindowTracksInstanceID(t *testing.T) {
	window := newSequenceWindow(4)

	if window.seenBefore("instance-a", 7) {
		t.Fatal("first sequence from instance-a should be accepted")
	}
	if !window.seenBefore("instance-a", 7) {
		t.Fatal("duplicate sequence from instance-a should be rejected")
	}
	if window.seenBefore("instance-b", 7) {
		t.Fatal("same sequence from a different instance should be accepted")
	}
}

func TestDispatchQueuesReadyWatchSession(t *testing.T) {
	session := &watchSession{
		sessionBase: &sessionBase{id: "watch-ready", closed: make(chan struct{})},
		outbound:    make(chan *relaypb.Envelope, 1),
	}
	session.outboundReady.Store(true)

	server := &Server{}
	envelope := &relaypb.Envelope{Body: &relaypb.Envelope_ServerNotice{ServerNotice: &relaypb.ServerNotice{Code: "ok"}}}
	server.dispatch([]outboundNotification{{session: session, envelope: envelope}})

	select {
	case queued := <-session.outbound:
		if queued != envelope {
			t.Fatal("unexpected envelope queued")
		}
	default:
		t.Fatal("expected envelope to be queued")
	}
}

func TestDispatchClosesBackpressuredWatchSession(t *testing.T) {
	session := &watchSession{
		sessionBase: &sessionBase{id: "watch-full", closed: make(chan struct{})},
		outbound:    make(chan *relaypb.Envelope, 1),
	}
	session.outboundReady.Store(true)
	session.outbound <- &relaypb.Envelope{}

	server := &Server{}
	server.dispatch([]outboundNotification{{session: session, envelope: &relaypb.Envelope{}}})

	select {
	case <-session.closed:
	default:
		t.Fatal("expected backpressured watch session to be closed")
	}
}

func TestDispatchSkipsInactiveWatchSession(t *testing.T) {
	session := &watchSession{
		sessionBase: &sessionBase{id: "watch-inactive", closed: make(chan struct{})},
		outbound:    make(chan *relaypb.Envelope, 1),
	}

	server := &Server{}
	server.dispatch([]outboundNotification{{session: session, envelope: &relaypb.Envelope{}}})

	select {
	case <-session.closed:
		t.Fatal("inactive watch session should not be closed")
	default:
	}
}
