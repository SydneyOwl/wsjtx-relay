package relay

import (
	"fmt"
	"strings"

	relaypb "github.com/SydneyOwl/wsjtx-relay-proto/gen/go/v20260405"
	wsjtx "github.com/k0swe/wsjtx-go/v4"
)

func mapWsjtxMessage(sourceName string, message any) *relaypb.Envelope {
	switch typed := message.(type) {
	case wsjtx.HeartbeatMessage:
		return wrapSessionActivity(sourceName, typed.Id)
	case wsjtx.ClearMessage:
		return wrapSessionActivity(sourceName, typed.Id)
	case wsjtx.CloseMessage:
		return wrapSessionActivity(sourceName, typed.Id)
	case wsjtx.LoggedAdifMessage:
		return wrapSessionActivity(sourceName, typed.Id)
	case wsjtx.DecodeMessage:
		return &relaypb.Envelope{
			Body: &relaypb.Envelope_Decode{
				Decode: &relaypb.DecodeEvent{
					SourceName:        sourceName,
					ClientId:          typed.Id,
					SessionEndpoint:   relayEndpoint(sourceName, typed.Id),
					IsNew:             typed.New,
					TimeMilliseconds:  int64(typed.Time),
					Snr:               typed.Snr,
					OffsetTimeSeconds: typed.DeltaTimeSec,
					OffsetFrequencyHz: int32(typed.DeltaFrequencyHz),
					Mode:              typed.Mode,
					Message:           typed.Message,
					LowConfidence:     typed.LowConfidence,
					OffAir:            typed.OffAir,
				},
			},
		}
	case wsjtx.WSPRDecodeMessage:
		detail := fmt.Sprintf("%ddBm", typed.Power)
		messageText := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(typed.Callsign), strings.TrimSpace(typed.Grid), detail}, " "))
		return &relaypb.Envelope{
			Body: &relaypb.Envelope_Decode{
				Decode: &relaypb.DecodeEvent{
					SourceName:          sourceName,
					ClientId:            typed.Id,
					SessionEndpoint:     relayEndpoint(sourceName, typed.Id),
					IsNew:               typed.New,
					TimeMilliseconds:    int64(typed.Time),
					Snr:                 typed.Snr,
					OffsetTimeSeconds:   typed.DeltaTime,
					OffsetFrequencyHz:   typed.Drift,
					Mode:                "WSPR",
					Message:             messageText,
					OffAir:              typed.OffAir,
					RemoteCallsign:      strings.TrimSpace(typed.Callsign),
					RemoteGrid:          strings.TrimSpace(typed.Grid),
					DetailText:          detail,
					ReportedFrequencyHz: float64(typed.Frequency),
				},
			},
		}
	case wsjtx.StatusMessage:
		return &relaypb.Envelope{
			Body: &relaypb.Envelope_Status{
				Status: &relaypb.StatusEvent{
					SourceName:      sourceName,
					ClientId:        typed.Id,
					SessionEndpoint: relayEndpoint(sourceName, typed.Id),
					Mode:            typed.Mode,
					TxMode:          typed.TxMode,
					Transmitting:    typed.Transmitting,
					TransmitMessage: typed.TxMessage,
					DialFrequencyHz: float64(typed.DialFrequency),
				},
			},
		}
	case wsjtx.QsoLoggedMessage:
		return &relaypb.Envelope{
			Body: &relaypb.Envelope_QsoLogged{
				QsoLogged: &relaypb.QsoLoggedEvent{
					SourceName:      sourceName,
					ClientId:        typed.Id,
					SessionEndpoint: relayEndpoint(sourceName, typed.Id),
					DxCall:          typed.DxCall,
					FrequencyHz:     float64(typed.TxFrequency),
				},
			},
		}
	default:
		return nil
	}
}

func wrapSessionActivity(sourceName, clientID string) *relaypb.Envelope {
	return &relaypb.Envelope{
		Body: &relaypb.Envelope_SessionActivity{
			SessionActivity: &relaypb.SessionActivityEvent{
				SourceName:      sourceName,
				ClientId:        clientID,
				SessionEndpoint: relayEndpoint(sourceName, clientID),
			},
		},
	}
}

func relayEndpoint(sourceName, clientID string) string {
	return fmt.Sprintf("relay:///%s/%s", sourceName, clientID)
}
