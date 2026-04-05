package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"time"
)

type ValidationResult struct {
	Valid         bool
	TimestampSkew bool
}

func BuildProof(sharedSecret string, nonce []byte, role string, tenantID string, sourceName string, instanceID string, timestampUnix int64) []byte {
	payload := buildPayload(nonce, role, tenantID, sourceName, instanceID, timestampUnix)
	mac := hmac.New(sha256.New, []byte(sharedSecret))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func ValidateProof(sharedSecret string, nonce []byte, role string, tenantID string, sourceName string, instanceID string, timestampUnix int64, proof []byte, maxSkew time.Duration) ValidationResult {
	now := time.Now().Unix()
	skew := now - timestampUnix
	if skew < 0 {
		skew = -skew
	}
	if time.Duration(skew)*time.Second > maxSkew {
		return ValidationResult{Valid: false, TimestampSkew: true}
	}

	expected := BuildProof(sharedSecret, nonce, role, tenantID, sourceName, instanceID, timestampUnix)
	return ValidationResult{Valid: hmac.Equal(expected, proof)}
}

func buildPayload(nonce []byte, role string, tenantID string, sourceName string, instanceID string, timestampUnix int64) []byte {
	totalLen := len(nonce) + 4 + len(role) + 4 + len(tenantID) + 4 + len(sourceName) + 4 + len(instanceID) + 8
	payload := make([]byte, 0, totalLen)
	payload = append(payload, nonce...)
	payload = appendLengthPrefixedString(payload, role)
	payload = appendLengthPrefixedString(payload, tenantID)
	payload = appendLengthPrefixedString(payload, sourceName)
	payload = appendLengthPrefixedString(payload, instanceID)

	timestampBuffer := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBuffer, uint64(timestampUnix))
	payload = append(payload, timestampBuffer...)
	return payload
}

func appendLengthPrefixedString(buffer []byte, value string) []byte {
	lengthBuffer := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuffer, uint32(len(value)))
	buffer = append(buffer, lengthBuffer...)
	buffer = append(buffer, value...)
	return buffer
}
