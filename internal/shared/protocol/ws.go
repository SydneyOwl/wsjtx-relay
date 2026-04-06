package protocol

import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	relaypb "github.com/SydneyOwl/wsjtx-relay-proto/gen/go/v20260405"
)

const (
	ProtoVersion     uint32 = 1
	MaxEnvelopeBytes int64  = 1 << 20
)

func ReadEnvelope(conn *websocket.Conn, readTimeout time.Duration) (*relaypb.Envelope, error) {
	if readTimeout > 0 {
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return nil, fmt.Errorf("set read deadline: %w", err)
		}
	} else {
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			return nil, fmt.Errorf("clear read deadline: %w", err)
		}
	}

	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	if messageType != websocket.BinaryMessage {
		return nil, fmt.Errorf("unsupported websocket message type: %d", messageType)
	}

	envelope := &relaypb.Envelope{}
	if err := proto.Unmarshal(payload, envelope); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return envelope, nil
}

func WriteEnvelope(conn *websocket.Conn, envelope *relaypb.Envelope, writeTimeout time.Duration) error {
	envelope.ProtoVersion = ProtoVersion
	payload, err := proto.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		return fmt.Errorf("write websocket message: %w", err)
	}
	return nil
}
