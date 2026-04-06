package relay

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	relaypb "github.com/SydneyOwl/wsjtx-relay-proto/gen/go/v20260405"
	"github.com/gorilla/websocket"
	wsjtx "github.com/k0swe/wsjtx-go/v4"

	"github.com/sydneyowl/wsjtx-relay/internal/client/config"
	"github.com/sydneyowl/wsjtx-relay/internal/client/tofu"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/auth"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/protocol"
)

const (
	writeTimeout        = 10 * time.Second
	relayEventQueueSize = 512
)

type Client struct {
	cfg config.Config

	instanceID string
	sourceSeq  atomic.Uint64
}

type connectionState struct {
	conn              *websocket.Conn
	writeMu           sync.Mutex
	seq               atomic.Uint64
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
	lastIncomingUnix  atomic.Int64
}

type liveEventDispatcher struct {
	mu     sync.RWMutex
	active chan *relaypb.Envelope
}

func New(cfg config.Config) *Client {
	instanceID := cfg.InstanceID
	if instanceID == "" {
		instanceID = randomHex(16)
	}
	return &Client{cfg: cfg, instanceID: instanceID}
}

func (c *Client) Run(ctx context.Context) error {
	dispatcher := &liveEventDispatcher{}
	if err := c.startWsjtxListener(ctx, dispatcher); err != nil {
		return err
	}

	backoffSchedule := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}
	attempt := 0

	for {
		if ctx.Err() != nil {
			return nil
		}

		state, err := c.connect(ctx)
		if err != nil {
			wait := backoffSchedule[min(attempt, len(backoffSchedule)-1)]
			attempt++
			log.Printf("relay connect failed: %v; retrying in %s", err, wait)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
				continue
			}
		}

		attempt = 0
		liveEvents := make(chan *relaypb.Envelope, relayEventQueueSize)
		dispatcher.activate(liveEvents)
		log.Printf("relay ingest connected to %s as %s/%s", c.cfg.ServerURL, c.cfg.TenantID, c.cfg.SourceName)
		err = c.runConnected(ctx, state, liveEvents)
		dispatcher.deactivate(liveEvents)
		if err != nil && ctx.Err() == nil {
			log.Printf("relay connection dropped: %v", err)
		}
	}
}

func (c *Client) connect(ctx context.Context) (*connectionState, error) {
	trustStore := tofu.NewStore(c.cfg.TrustStorePath, c.cfg.AutoTrustOnFirstUse)
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
		VerifyConnection: func(connectionState tls.ConnectionState) error {
			return trustStore.Verify(connectionState)
		},
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		TLSClientConfig:  tlsConfig,
	}

	conn, _, err := dialer.DialContext(ctx, c.cfg.ServerURL+"/v1/ingest", nil)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}
	conn.SetReadLimit(protocol.MaxEnvelopeBytes)

	state := &connectionState{conn: conn}
	state.touch()

	hello := &relaypb.ClientHello{
		Role:              "ingest",
		TenantId:          c.cfg.TenantID,
		SourceName:        c.cfg.SourceName,
		InstanceId:        c.instanceID,
		ClientName:        c.cfg.ClientName,
		ClientVersion:     c.cfg.ClientVersion,
		SourceDisplayName: c.cfg.SourceDisplayName,
	}
	if err := state.writeEnvelope(&relaypb.Envelope{Body: &relaypb.Envelope_ClientHello{ClientHello: hello}}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("send client hello: %w", err)
	}

	serverHelloEnvelope, err := protocol.ReadEnvelope(conn, 15*time.Second)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read server hello: %w", err)
	}
	serverHello := serverHelloEnvelope.GetServerHello()
	if serverHello == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("expected server_hello, got %T", serverHelloEnvelope.Body)
	}

	state.heartbeatInterval = time.Duration(serverHello.HeartbeatIntervalSec) * time.Second
	state.heartbeatTimeout = time.Duration(serverHello.HeartbeatTimeoutSec) * time.Second
	if state.heartbeatInterval <= 0 {
		state.heartbeatInterval = 10 * time.Second
	}
	if state.heartbeatTimeout < state.heartbeatInterval {
		state.heartbeatTimeout = 30 * time.Second
	}

	timestampUnix := time.Now().Unix()
	proof := auth.BuildProof(c.cfg.SharedSecret, serverHello.Nonce, "ingest", c.cfg.TenantID, c.cfg.SourceName, c.instanceID, timestampUnix)
	if err := state.writeEnvelope(&relaypb.Envelope{Body: &relaypb.Envelope_AuthRequest{AuthRequest: &relaypb.AuthRequest{TimestampUnix: timestampUnix, Proof: proof}}}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("send auth request: %w", err)
	}

	authEnvelope, err := protocol.ReadEnvelope(conn, 15*time.Second)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read auth result: %w", err)
	}
	authResult := authEnvelope.GetAuthResult()
	if authResult == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("expected auth_result, got %T", authEnvelope.Body)
	}
	if !authResult.Ok {
		_ = conn.Close()
		return nil, fmt.Errorf("relay auth failed: %s (%s)", authResult.Message, authResult.ErrorCode)
	}

	return state, nil
}

func (c *Client) runConnected(ctx context.Context, state *connectionState, events <-chan *relaypb.Envelope) error {
	defer state.close()

	readErrCh := make(chan error, 1)
	go c.readLoop(state, readErrCh)
	ticker := time.NewTicker(state.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErrCh:
			return err
		case <-ticker.C:
			if time.Since(state.lastIncomingTime()) > state.heartbeatTimeout {
				return fmt.Errorf("heartbeat timeout")
			}
			if err := state.writeEnvelope(&relaypb.Envelope{Body: &relaypb.Envelope_Ping{Ping: &relaypb.Ping{TimestampUnixMs: time.Now().UnixMilli()}}}); err != nil {
				return err
			}
		case envelope := <-events:
			if envelope == nil {
				continue
			}
			envelope.Seq = c.sourceSeq.Add(1)
			if err := state.writeEnvelope(envelope); err != nil {
				return err
			}
		}
	}
}

func (c *Client) readLoop(state *connectionState, readErrCh chan<- error) {
	defer close(readErrCh)

	for {
		envelope, err := protocol.ReadEnvelope(state.conn, 0)
		if err != nil {
			readErrCh <- err
			return
		}
		state.touch()

		switch body := envelope.Body.(type) {
		case *relaypb.Envelope_Ping:
			if err := state.writeEnvelope(&relaypb.Envelope{Body: &relaypb.Envelope_Pong{Pong: &relaypb.Pong{TimestampUnixMs: body.Ping.TimestampUnixMs}}}); err != nil {
				readErrCh <- err
				return
			}
		case *relaypb.Envelope_Pong:
		case *relaypb.Envelope_ServerNotice:
			log.Printf("server notice [%s/%s]: %s", body.ServerNotice.Level, body.ServerNotice.Code, body.ServerNotice.Message)
		case *relaypb.Envelope_SourceState:
			log.Printf("unexpected source_state for ingest: %s", body.SourceState.SourceName)
		default:
			log.Printf("unexpected server frame for ingest: %T", body)
		}
	}
}

func (c *Client) startWsjtxListener(ctx context.Context, dispatcher *liveEventDispatcher) error {
	addr, err := net.ResolveUDPAddr("udp", c.cfg.UDPListenAddr)
	if err != nil {
		return fmt.Errorf("resolve UDP listen address: %w", err)
	}
	if addr.Port <= 0 {
		return fmt.Errorf("invalid UDP listen port: %d", addr.Port)
	}
	if addr.IP == nil {
		addr.IP = net.IPv4zero
	}

	server, err := wsjtx.MakeServerGiven(addr.IP, uint(addr.Port))
	if err != nil {
		return fmt.Errorf("listen for WSJT-X: %w", err)
	}

	msgCh := make(chan interface{}, 128)
	errCh := make(chan error, 128)
	go server.ListenToWsjtx(msgCh, errCh)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-errCh:
				if !ok {
					return
				}
				if err == nil || ctx.Err() != nil {
					continue
				}
				log.Printf("wsjtx listener error: %v", err)
			case message, ok := <-msgCh:
				if !ok {
					return
				}
				envelope := mapWsjtxMessage(c.cfg.SourceName, message)
				if envelope == nil {
					continue
				}
				delivered, connected := dispatcher.publish(envelope)
				if connected && !delivered {
					log.Printf("wsjtx event dropped because relay output queue is full")
				}
			}
		}
	}()

	log.Printf("listening for WSJT-X UDP on %s", server.LocalAddr())
	return nil
}

func (s *connectionState) writeEnvelope(envelope *relaypb.Envelope) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if envelope.Seq == 0 {
		envelope.Seq = s.seq.Add(1)
	}
	return protocol.WriteEnvelope(s.conn, envelope, writeTimeout)
}

func (s *connectionState) touch() {
	s.lastIncomingUnix.Store(time.Now().UnixMilli())
}

func (s *connectionState) lastIncomingTime() time.Time {
	return time.UnixMilli(s.lastIncomingUnix.Load())
}

func (s *connectionState) close() {
	_ = s.conn.Close()
}

func (d *liveEventDispatcher) activate(events chan *relaypb.Envelope) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.active = events
}

func (d *liveEventDispatcher) deactivate(events chan *relaypb.Envelope) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.active == events {
		d.active = nil
	}
}

func (d *liveEventDispatcher) publish(envelope *relaypb.Envelope) (delivered bool, connected bool) {
	d.mu.RLock()
	active := d.active
	d.mu.RUnlock()

	if active == nil {
		return false, false
	}

	select {
	case active <- envelope:
		return true, true
	default:
		return false, true
	}
}

func randomHex(size int) string {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
