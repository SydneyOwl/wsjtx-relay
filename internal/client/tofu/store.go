package tofu

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Store struct {
	path                string
	autoTrustOnFirstUse bool
}

func NewStore(path string, autoTrustOnFirstUse bool) *Store {
	return &Store{path: path, autoTrustOnFirstUse: autoTrustOnFirstUse}
}

func (s *Store) Verify(connectionState tls.ConnectionState) error {
	if len(connectionState.PeerCertificates) == 0 {
		return errors.New("server did not present a certificate")
	}

	fingerprint := Fingerprint(connectionState.PeerCertificates[0].RawSubjectPublicKeyInfo)
	trusted, err := s.Load()
	if err != nil {
		return err
	}
	if trusted == "" {
		if !s.autoTrustOnFirstUse {
			return fmt.Errorf("no trusted fingerprint configured; observed %s", fingerprint)
		}
		return s.Save(fingerprint)
	}
	if !strings.EqualFold(trusted, fingerprint) {
		return fmt.Errorf("server fingerprint mismatch: trusted=%s current=%s", trusted, fingerprint)
	}
	return nil
}

func (s *Store) Load() (string, error) {
	data, err := os.ReadFile(s.path)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return "", fmt.Errorf("read trust store: %w", err)
}

func (s *Store) Save(fingerprint string) error {
	if err := os.WriteFile(s.path, []byte(fingerprint+"\n"), 0o600); err != nil {
		return fmt.Errorf("write trust store: %w", err)
	}
	return nil
}

func Fingerprint(spki []byte) string {
	sum := sha256.Sum256(spki)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
