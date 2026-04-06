package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	relaypb "github.com/SydneyOwl/wsjtx-relay-proto/gen/go/v20260405"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/sydneyowl/wsjtx-relay/internal/server/config"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/auth"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/protocol"
)

const (
	writeTimeout           = 10 * time.Second
	watchOutboundQueueSize = 128
)

type Server struct {
	cfg              config.Config
	serverInstanceID string
	upgrader         websocket.Upgrader

	mu      sync.Mutex
	tenants map[string]*tenantRuntime
}

type tenantRuntime struct {
	id       string
	sources  map[string]*sourceRuntime
	watchers map[string]*watchSession
}

type sourceRuntime struct {
	name         string
	displayName  string
	lastSeen     time.Time
	lastStatus   *relaypb.StatusEvent
	activeIngest *ingestSession
	recentSeq    *sequenceWindow
}

type ingestSession struct {
	*sessionBase
	sourceName        string
	sourceDisplayName string
}

type watchSession struct {
	*sessionBase
	selectedSource string
	outbound       chan *relaypb.Envelope
	outboundReady  atomic.Bool
}

type sessionBase struct {
	id                string
	role              string
	tenantID          string
	instanceID        string
	conn              *websocket.Conn
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration

	writeMu      sync.Mutex
	closeOnce    sync.Once
	closed       chan struct{}
	incomingUnix atomic.Int64
	seq          atomic.Uint64
}

type sequenceWindow struct {
	max   int
	seen  map[sequenceKey]struct{}
	order []sequenceKey
}

type outboundNotification struct {
	session  *watchSession
	envelope *relaypb.Envelope
}

type sequenceKey struct {
	instanceID string
	seq        uint64
}

type enqueueResult uint8

const (
	enqueueAccepted enqueueResult = iota
	enqueueInactive
	enqueueBackpressured
)

func NewServer(cfg config.Config) *Server {
	return &Server{
		cfg:              cfg,
		serverInstanceID: randomHex(16),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  64 * 1024,
			WriteBufferSize: 64 * 1024,
			CheckOrigin:     func(*http.Request) bool { return true },
		},
		tenants: make(map[string]*tenantRuntime),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
		s.handleConnection(w, r, "ingest")
	})
	mux.HandleFunc("/v1/watch", func(w http.ResponseWriter, r *http.Request) {
		s.handleConnection(w, r, "watch")
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func (s *Server) handleConnection(w http.ResponseWriter, r *http.Request, expectedRole string) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade %s connection failed: %v", expectedRole, err)
		return
	}

	conn.SetReadLimit(protocol.MaxEnvelopeBytes)

	helloEnv, err := protocol.ReadEnvelope(conn, 15*time.Second)
	if err != nil {
		log.Printf("read client hello failed: %v", err)
		_ = conn.Close()
		return
	}
	if helloEnv.ProtoVersion != 0 && helloEnv.ProtoVersion != protocol.ProtoVersion {
		s.writeHandshakeError(conn, "unsupported_proto_version", "unsupported protocol version")
		return
	}

	hello := helloEnv.GetClientHello()
	if hello == nil {
		s.writeHandshakeError(conn, "invalid_request", "expected client_hello as first frame")
		return
	}

	if strings.TrimSpace(hello.Role) != expectedRole {
		s.writeHandshakeError(conn, "invalid_role", "role does not match endpoint")
		return
	}
	if strings.TrimSpace(hello.TenantId) == "" {
		s.writeHandshakeError(conn, "missing_tenant_id", "tenant_id is required")
		return
	}
	if expectedRole == "ingest" && strings.TrimSpace(hello.SourceName) == "" {
		s.writeHandshakeError(conn, "missing_source_name", "source_name is required")
		return
	}

	nonce := randomBytes(32)
	if err := protocol.WriteEnvelope(conn, &relaypb.Envelope{
		Body: &relaypb.Envelope_ServerHello{
			ServerHello: &relaypb.ServerHello{
				ServerInstanceId:     s.serverInstanceID,
				Nonce:                nonce,
				HeartbeatIntervalSec: uint32(s.cfg.HeartbeatInterval / time.Second),
				HeartbeatTimeoutSec:  uint32(s.cfg.HeartbeatTimeout / time.Second),
			},
		},
	}, writeTimeout); err != nil {
		log.Printf("write server hello failed: %v", err)
		_ = conn.Close()
		return
	}

	authEnv, err := protocol.ReadEnvelope(conn, 15*time.Second)
	if err != nil {
		log.Printf("read auth request failed: %v", err)
		_ = conn.Close()
		return
	}
	authRequest := authEnv.GetAuthRequest()
	if authRequest == nil {
		s.writeHandshakeError(conn, "invalid_request", "expected auth_request")
		return
	}

	validation := auth.ValidateProof(
		s.cfg.SharedSecret,
		nonce,
		hello.Role,
		hello.TenantId,
		hello.SourceName,
		hello.InstanceId,
		authRequest.TimestampUnix,
		authRequest.Proof,
		s.cfg.MaxTimestampSkew)
	if !validation.Valid {
		if validation.TimestampSkew {
			s.writeHandshakeError(conn, "timestamp_skew", "auth timestamp outside tolerated skew")
		} else {
			s.writeHandshakeError(conn, "auth_failed", "authentication failed")
		}
		return
	}

	base := &sessionBase{
		id:                randomHex(16),
		role:              expectedRole,
		tenantID:          hello.TenantId,
		instanceID:        hello.InstanceId,
		conn:              conn,
		heartbeatInterval: s.cfg.HeartbeatInterval,
		heartbeatTimeout:  s.cfg.HeartbeatTimeout,
		closed:            make(chan struct{}),
	}
	base.touch()

	if expectedRole == "ingest" {
		ingest := &ingestSession{
			sessionBase:       base,
			sourceName:        strings.TrimSpace(hello.SourceName),
			sourceDisplayName: strings.TrimSpace(hello.SourceDisplayName),
		}
		if ingest.sourceDisplayName == "" {
			ingest.sourceDisplayName = ingest.sourceName
		}
		if errCode, errMessage := s.registerIngest(ingest); errCode != "" {
			s.writeHandshakeError(conn, errCode, errMessage)
			return
		}
		if err := ingest.sendEnvelope(&relaypb.Envelope{
			Body: &relaypb.Envelope_AuthResult{AuthResult: &relaypb.AuthResult{Ok: true, SessionId: ingest.id}},
		}); err != nil {
			log.Printf("write ingest auth success failed: %v", err)
			s.unregisterIngest(ingest, false)
			return
		}
		go s.monitorSession(ingest.sessionBase)
		s.runIngestLoop(ingest)
		return
	}

	watch := &watchSession{
		sessionBase: base,
		outbound:    make(chan *relaypb.Envelope, watchOutboundQueueSize),
	}
	if err := watch.sendEnvelope(&relaypb.Envelope{
		Body: &relaypb.Envelope_AuthResult{AuthResult: &relaypb.AuthResult{Ok: true, SessionId: watch.id}},
	}); err != nil {
		log.Printf("write watch auth success failed: %v", err)
		return
	}
	catalog := s.registerWatch(watch)
	if catalog != nil {
		if err := watch.sendEnvelope(&relaypb.Envelope{Body: &relaypb.Envelope_SourceCatalog{SourceCatalog: catalog}}); err != nil {
			log.Printf("write initial source catalog failed: %v", err)
			s.unregisterWatch(watch)
			return
		}
	}
	watch.outboundReady.Store(true)
	go s.runWatchWriter(watch)
	go s.monitorSession(watch.sessionBase)
	s.runWatchLoop(watch)
}

func (s *Server) writeHandshakeError(conn *websocket.Conn, code string, message string) {
	_ = protocol.WriteEnvelope(conn, &relaypb.Envelope{
		Body: &relaypb.Envelope_AuthResult{AuthResult: &relaypb.AuthResult{Ok: false, ErrorCode: code, Message: message}},
	}, writeTimeout)
	_ = conn.Close()
}

func (s *Server) runIngestLoop(session *ingestSession) {
	defer func() {
		session.close()
		s.unregisterIngest(session, false)
	}()

	for {
		envelope, err := protocol.ReadEnvelope(session.conn, 0)
		if err != nil {
			if !errors.Is(err, websocket.ErrCloseSent) {
				log.Printf("ingest session %s closed: %v", session.id, err)
			}
			return
		}
		session.touch()

		switch body := envelope.Body.(type) {
		case *relaypb.Envelope_Ping:
			_ = session.sendEnvelope(&relaypb.Envelope{Body: &relaypb.Envelope_Pong{Pong: &relaypb.Pong{TimestampUnixMs: body.Ping.TimestampUnixMs}}})
		case *relaypb.Envelope_Pong:
		case *relaypb.Envelope_SessionActivity:
			s.handleSessionActivity(session, envelope.Seq, body.SessionActivity)
		case *relaypb.Envelope_Decode:
			s.handleDecodeEvent(session, envelope.Seq, body.Decode)
		case *relaypb.Envelope_Status:
			s.handleStatusEvent(session, envelope.Seq, body.Status)
		case *relaypb.Envelope_QsoLogged:
			s.handleQsoLoggedEvent(session, envelope.Seq, body.QsoLogged)
		default:
			_ = session.sendEnvelope(serverNoticeEnvelope("warn", "invalid_request", "unsupported ingest frame"))
		}
	}
}

func (s *Server) runWatchLoop(session *watchSession) {
	defer func() {
		session.close()
		s.unregisterWatch(session)
	}()

	for {
		envelope, err := protocol.ReadEnvelope(session.conn, 0)
		if err != nil {
			if !errors.Is(err, websocket.ErrCloseSent) {
				log.Printf("watch session %s closed: %v", session.id, err)
			}
			return
		}
		session.touch()

		switch body := envelope.Body.(type) {
		case *relaypb.Envelope_Ping:
			_ = session.sendEnvelope(&relaypb.Envelope{Body: &relaypb.Envelope_Pong{Pong: &relaypb.Pong{TimestampUnixMs: body.Ping.TimestampUnixMs}}})
		case *relaypb.Envelope_Pong:
		case *relaypb.Envelope_SelectSourceRequest:
			notifications := s.selectSource(session, strings.TrimSpace(body.SelectSourceRequest.SourceName))
			s.dispatch(notifications)
		default:
			_ = session.sendEnvelope(serverNoticeEnvelope("warn", "invalid_request", "unsupported watch frame"))
		}
	}
}

func (s *Server) monitorSession(session *sessionBase) {
	ticker := time.NewTicker(session.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Since(session.lastIncomingTime()) > session.heartbeatTimeout {
				log.Printf("session %s timed out", session.id)
				session.close()
				return
			}
			if err := session.sendEnvelope(&relaypb.Envelope{Body: &relaypb.Envelope_Ping{Ping: &relaypb.Ping{TimestampUnixMs: time.Now().UnixMilli()}}}); err != nil {
				session.close()
				return
			}
		case <-session.closed:
			return
		}
	}
}

func (s *Server) runWatchWriter(session *watchSession) {
	for {
		select {
		case <-session.closed:
			return
		case envelope := <-session.outbound:
			if envelope == nil {
				continue
			}
			if err := session.sendEnvelope(envelope); err != nil {
				log.Printf("send envelope to watch session %s failed: %v", session.id, err)
				session.close()
				return
			}
		}
	}
}

func (s *Server) registerIngest(session *ingestSession) (string, string) {
	s.mu.Lock()
	tenant := s.getOrCreateTenantLocked(session.tenantID)
	source := tenant.sources[session.sourceName]
	if source == nil {
		source = &sourceRuntime{
			name:        session.sourceName,
			displayName: session.sourceDisplayName,
			recentSeq:   newSequenceWindow(1024),
		}
		tenant.sources[session.sourceName] = source
	}

	var replaced *ingestSession
	if source.activeIngest != nil {
		if source.activeIngest.instanceID != session.instanceID {
			s.mu.Unlock()
			return "source_busy", "source is already owned by another ingest instance"
		}
		replaced = source.activeIngest
	}

	source.activeIngest = session
	source.displayName = session.sourceDisplayName
	source.lastSeen = time.Now().UTC()
	notifications := s.buildTenantCatalogNotificationsLocked(tenant)
	state := &relaypb.SourceStateEvent{SourceName: session.sourceName, State: relaypb.SourceStateEvent_ONLINE}
	if replaced != nil {
		state.State = relaypb.SourceStateEvent_REPLACED
		state.Message = "existing ingest connection replaced"
	}
	notifications = append(notifications, s.buildTenantStateNotificationsLocked(tenant, state)...)
	s.mu.Unlock()

	if replaced != nil {
		replaced.close()
	}
	s.dispatch(notifications)
	return "", ""
}

func (s *Server) unregisterIngest(session *ingestSession, replaced bool) {
	s.mu.Lock()
	tenant := s.tenants[session.tenantID]
	if tenant == nil {
		s.mu.Unlock()
		return
	}
	source := tenant.sources[session.sourceName]
	if source == nil || source.activeIngest != session {
		s.cleanupTenantLocked(session.tenantID)
		s.mu.Unlock()
		return
	}

	source.activeIngest = nil
	source.lastSeen = time.Now().UTC()
	notifications := s.buildTenantCatalogNotificationsLocked(tenant)
	if !replaced {
		state := &relaypb.SourceStateEvent{SourceName: session.sourceName, State: relaypb.SourceStateEvent_OFFLINE, Message: "ingest disconnected"}
		notifications = append(notifications, s.buildTenantStateNotificationsLocked(tenant, state)...)
	}
	s.cleanupTenantLocked(session.tenantID)
	s.mu.Unlock()
	s.dispatch(notifications)
}

func (s *Server) registerWatch(session *watchSession) *relaypb.SourceCatalog {
	s.mu.Lock()
	defer s.mu.Unlock()

	tenant := s.getOrCreateTenantLocked(session.tenantID)
	tenant.watchers[session.id] = session
	return s.buildCatalogLocked(tenant, session.selectedSource)
}

func (s *Server) unregisterWatch(session *watchSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tenant := s.tenants[session.tenantID]
	if tenant == nil {
		return
	}
	delete(tenant.watchers, session.id)
	s.cleanupTenantLocked(session.tenantID)
}

func (s *Server) selectSource(session *watchSession, sourceName string) []outboundNotification {
	s.mu.Lock()
	defer s.mu.Unlock()

	tenant := s.tenants[session.tenantID]
	if tenant == nil {
		return []outboundNotification{{session: session, envelope: selectResultEnvelope(false, sourceName, "source_not_found", "tenant has no known sources")}}
	}
	source := tenant.sources[sourceName]
	if source == nil {
		return []outboundNotification{{session: session, envelope: selectResultEnvelope(false, sourceName, "source_not_found", "source not found")}}
	}

	session.selectedSource = sourceName
	snapshot := buildSnapshot(source)
	catalog := s.buildCatalogLocked(tenant, sourceName)
	notifications := []outboundNotification{{session: session, envelope: selectResultEnvelope(true, sourceName, "", "")}}
	if catalog != nil {
		notifications = append(notifications, outboundNotification{session: session, envelope: &relaypb.Envelope{Body: &relaypb.Envelope_SourceCatalog{SourceCatalog: catalog}}})
	}
	notifications = append(notifications, outboundNotification{session: session, envelope: &relaypb.Envelope{Body: &relaypb.Envelope_SourceSnapshot{SourceSnapshot: snapshot}}})
	return notifications
}

func (s *Server) withIngestEvent(session *ingestSession, seq uint64, build func(source *sourceRuntime, watchers []*watchSession) []outboundNotification) []outboundNotification {
	s.mu.Lock()
	defer s.mu.Unlock()

	tenant := s.tenants[session.tenantID]
	if tenant == nil {
		return nil
	}
	source := tenant.sources[session.sourceName]
	if source == nil || source.activeIngest != session {
		return nil
	}

	if seq != 0 && source.recentSeq.seenBefore(session.instanceID, seq) {
		return nil
	}
	source.lastSeen = time.Now().UTC()

	watchers := s.selectedWatchersLocked(tenant, session.sourceName)
	return build(source, watchers)
}

func (s *Server) handleSessionActivity(session *ingestSession, seq uint64, activity *relaypb.SessionActivityEvent) {
	if activity == nil {
		return
	}

	notifications := s.withIngestEvent(session, seq, func(_ *sourceRuntime, watchers []*watchSession) []outboundNotification {
		if len(watchers) == 0 {
			return nil
		}
		return makeSessionActivityNotifications(watchers, session.sourceName, strings.TrimSpace(activity.ClientId))
	})
	s.dispatch(notifications)
}

func (s *Server) handleDecodeEvent(session *ingestSession, seq uint64, event *relaypb.DecodeEvent) {
	decode := sanitizeDecodeEvent(session.sourceName, event)
	if decode == nil {
		return
	}

	notifications := s.withIngestEvent(session, seq, func(_ *sourceRuntime, watchers []*watchSession) []outboundNotification {
		if len(watchers) == 0 {
			return nil
		}
		return wrapWatchNotifications(watchers, &relaypb.Envelope{Body: &relaypb.Envelope_Decode{Decode: decode}})
	})
	s.dispatch(notifications)
}

func (s *Server) handleStatusEvent(session *ingestSession, seq uint64, event *relaypb.StatusEvent) {
	status := sanitizeStatusEvent(session.sourceName, event)
	if status == nil {
		return
	}

	notifications := s.withIngestEvent(session, seq, func(source *sourceRuntime, watchers []*watchSession) []outboundNotification {
		source.lastStatus = proto.Clone(status).(*relaypb.StatusEvent)
		if len(watchers) == 0 {
			return nil
		}
		return wrapWatchNotifications(watchers, &relaypb.Envelope{Body: &relaypb.Envelope_Status{Status: status}})
	})
	s.dispatch(notifications)
}

func (s *Server) handleQsoLoggedEvent(session *ingestSession, seq uint64, event *relaypb.QsoLoggedEvent) {
	qso := sanitizeQsoLoggedEvent(session.sourceName, event)
	if qso == nil {
		return
	}

	notifications := s.withIngestEvent(session, seq, func(_ *sourceRuntime, watchers []*watchSession) []outboundNotification {
		if len(watchers) == 0 {
			return nil
		}
		return wrapWatchNotifications(watchers, &relaypb.Envelope{Body: &relaypb.Envelope_QsoLogged{QsoLogged: qso}})
	})
	s.dispatch(notifications)
}

func makeSessionActivityNotifications(watchers []*watchSession, sourceName string, clientID string) []outboundNotification {
	event := &relaypb.SessionActivityEvent{
		SourceName:      sourceName,
		ClientId:        clientID,
		SessionEndpoint: relayEndpoint("", sourceName, clientID),
	}
	return wrapWatchNotifications(watchers, &relaypb.Envelope{Body: &relaypb.Envelope_SessionActivity{SessionActivity: event}})
}

func sanitizeDecodeEvent(sourceName string, event *relaypb.DecodeEvent) *relaypb.DecodeEvent {
	if event == nil {
		return nil
	}
	clientID := strings.TrimSpace(event.ClientId)
	return &relaypb.DecodeEvent{
		SourceName:          sourceName,
		ClientId:            clientID,
		SessionEndpoint:     relayEndpoint("", sourceName, clientID),
		IsNew:               event.IsNew,
		TimeMilliseconds:    event.TimeMilliseconds,
		Snr:                 event.Snr,
		OffsetTimeSeconds:   event.OffsetTimeSeconds,
		OffsetFrequencyHz:   event.OffsetFrequencyHz,
		Mode:                event.Mode,
		Message:             event.Message,
		LowConfidence:       event.LowConfidence,
		OffAir:              event.OffAir,
		RemoteCallsign:      strings.TrimSpace(event.RemoteCallsign),
		RemoteGrid:          strings.TrimSpace(event.RemoteGrid),
		DetailText:          event.DetailText,
		ReportedFrequencyHz: event.ReportedFrequencyHz,
	}
}

func sanitizeStatusEvent(sourceName string, event *relaypb.StatusEvent) *relaypb.StatusEvent {
	if event == nil {
		return nil
	}
	clientID := strings.TrimSpace(event.ClientId)
	return &relaypb.StatusEvent{
		SourceName:      sourceName,
		ClientId:        clientID,
		SessionEndpoint: relayEndpoint("", sourceName, clientID),
		Mode:            event.Mode,
		TxMode:          event.TxMode,
		Transmitting:    event.Transmitting,
		TransmitMessage: event.TransmitMessage,
		DialFrequencyHz: event.DialFrequencyHz,
	}
}

func sanitizeQsoLoggedEvent(sourceName string, event *relaypb.QsoLoggedEvent) *relaypb.QsoLoggedEvent {
	if event == nil {
		return nil
	}
	clientID := strings.TrimSpace(event.ClientId)
	return &relaypb.QsoLoggedEvent{
		SourceName:      sourceName,
		ClientId:        clientID,
		SessionEndpoint: relayEndpoint("", sourceName, clientID),
		DxCall:          strings.TrimSpace(event.DxCall),
		Band:            event.Band,
		FrequencyHz:     event.FrequencyHz,
	}
}

func relayEndpoint(tenantID string, sourceName string, clientID string) string {
	if tenantID == "" {
		return fmt.Sprintf("relay:///%s/%s", sourceName, clientID)
	}
	return fmt.Sprintf("relay://%s/%s/%s", tenantID, sourceName, clientID)
}

func wrapWatchNotifications(watchers []*watchSession, envelope *relaypb.Envelope) []outboundNotification {
	notifications := make([]outboundNotification, 0, len(watchers))
	for _, watcher := range watchers {
		notifications = append(notifications, outboundNotification{session: watcher, envelope: envelope})
	}
	return notifications
}

func selectResultEnvelope(ok bool, sourceName string, code string, message string) *relaypb.Envelope {
	return &relaypb.Envelope{Body: &relaypb.Envelope_SelectSourceResult{SelectSourceResult: &relaypb.SelectSourceResult{Ok: ok, SourceName: sourceName, ErrorCode: code, Message: message}}}
}

func serverNoticeEnvelope(level string, code string, message string) *relaypb.Envelope {
	return &relaypb.Envelope{Body: &relaypb.Envelope_ServerNotice{ServerNotice: &relaypb.ServerNotice{Level: level, Code: code, Message: message}}}
}

func buildSnapshot(source *sourceRuntime) *relaypb.SourceSnapshot {
	snapshot := &relaypb.SourceSnapshot{
		SourceName:   source.name,
		SourceOnline: source.activeIngest != nil,
		ServerUnixMs: time.Now().UnixMilli(),
	}
	if source.lastStatus != nil {
		snapshot.LastStatus = proto.Clone(source.lastStatus).(*relaypb.StatusEvent)
	}
	return snapshot
}

func (s *Server) dispatch(notifications []outboundNotification) {
	for _, notification := range notifications {
		if notification.session == nil || notification.envelope == nil {
			continue
		}
		switch notification.session.enqueueEnvelope(notification.envelope) {
		case enqueueAccepted, enqueueInactive:
			continue
		case enqueueBackpressured:
			log.Printf("watch session %s outbound queue is full; closing session", notification.session.id)
			notification.session.close()
		}
	}
}

func (s *Server) buildTenantCatalogNotificationsLocked(tenant *tenantRuntime) []outboundNotification {
	notifications := make([]outboundNotification, 0, len(tenant.watchers))
	for _, watcher := range tenant.watchers {
		catalog := s.buildCatalogLocked(tenant, watcher.selectedSource)
		notifications = append(notifications, outboundNotification{session: watcher, envelope: &relaypb.Envelope{Body: &relaypb.Envelope_SourceCatalog{SourceCatalog: catalog}}})
	}
	return notifications
}

func (s *Server) buildTenantStateNotificationsLocked(tenant *tenantRuntime, event *relaypb.SourceStateEvent) []outboundNotification {
	notifications := make([]outboundNotification, 0, len(tenant.watchers))
	for _, watcher := range tenant.watchers {
		notifications = append(notifications, outboundNotification{session: watcher, envelope: &relaypb.Envelope{Body: &relaypb.Envelope_SourceState{SourceState: event}}})
	}
	return notifications
}

func (s *Server) buildCatalogLocked(tenant *tenantRuntime, currentSource string) *relaypb.SourceCatalog {
	sources := make([]*relaypb.SourceDescriptor, 0, len(tenant.sources))
	for _, source := range tenant.sources {
		displayName := source.displayName
		if displayName == "" {
			displayName = source.name
		}
		sources = append(sources, &relaypb.SourceDescriptor{
			SourceName:     source.name,
			DisplayName:    displayName,
			Online:         source.activeIngest != nil,
			LastSeenUnixMs: source.lastSeen.UnixMilli(),
		})
	}
	return &relaypb.SourceCatalog{Sources: sources, CurrentSourceName: currentSource}
}

func (s *Server) selectedWatchersLocked(tenant *tenantRuntime, sourceName string) []*watchSession {
	watchers := make([]*watchSession, 0, len(tenant.watchers))
	for _, watcher := range tenant.watchers {
		if watcher.selectedSource == sourceName {
			watchers = append(watchers, watcher)
		}
	}
	return watchers
}

func (s *Server) getOrCreateTenantLocked(tenantID string) *tenantRuntime {
	tenant := s.tenants[tenantID]
	if tenant == nil {
		tenant = &tenantRuntime{
			id:       tenantID,
			sources:  make(map[string]*sourceRuntime),
			watchers: make(map[string]*watchSession),
		}
		s.tenants[tenantID] = tenant
	}
	return tenant
}

func (s *Server) cleanupTenantLocked(tenantID string) {
	tenant := s.tenants[tenantID]
	if tenant == nil {
		return
	}
	if len(tenant.watchers) > 0 {
		return
	}
	for _, source := range tenant.sources {
		if source.activeIngest != nil {
			return
		}
	}
	delete(s.tenants, tenantID)
}

func newSequenceWindow(max int) *sequenceWindow {
	return &sequenceWindow{max: max, seen: make(map[sequenceKey]struct{})}
}

func (w *sequenceWindow) seenBefore(instanceID string, seq uint64) bool {
	key := sequenceKey{instanceID: instanceID, seq: seq}
	if _, ok := w.seen[key]; ok {
		return true
	}
	w.seen[key] = struct{}{}
	w.order = append(w.order, key)
	if len(w.order) > w.max {
		oldest := w.order[0]
		w.order = w.order[1:]
		delete(w.seen, oldest)
	}
	return false
}

func (s *sessionBase) sendEnvelope(envelope *relaypb.Envelope) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	select {
	case <-s.closed:
		return errors.New("session is closed")
	default:
	}

	if envelope == nil {
		return errors.New("envelope is nil")
	}
	if s.conn == nil {
		return errors.New("session connection is nil")
	}
	cloned := proto.Clone(envelope).(*relaypb.Envelope)
	if cloned.Seq == 0 {
		cloned.Seq = s.seq.Add(1)
	}
	return protocol.WriteEnvelope(s.conn, cloned, writeTimeout)
}

func (s *sessionBase) touch() {
	s.incomingUnix.Store(time.Now().UnixMilli())
}

func (s *sessionBase) lastIncomingTime() time.Time {
	return time.UnixMilli(s.incomingUnix.Load())
}

func (s *sessionBase) close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
}

func (s *watchSession) enqueueEnvelope(envelope *relaypb.Envelope) enqueueResult {
	if envelope == nil {
		return enqueueAccepted
	}
	if s.outbound == nil || !s.outboundReady.Load() {
		return enqueueInactive
	}

	select {
	case <-s.closed:
		return enqueueInactive
	default:
	}

	select {
	case s.outbound <- envelope:
		return enqueueAccepted
	case <-s.closed:
		return enqueueInactive
	default:
		return enqueueBackpressured
	}
}

func randomHex(size int) string {
	return hex.EncodeToString(randomBytes(size))
}

func randomBytes(size int) []byte {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return buffer
}
