package main

import (
	"crypto/tls"
	"log"
	"net/http"

	"github.com/sydneyowl/wsjtx-relay/internal/server/config"
	"github.com/sydneyowl/wsjtx-relay/internal/server/runtime"
	"github.com/sydneyowl/wsjtx-relay/internal/server/tlsutil"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	certificate, err := tlsutil.EnsureCertificate(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		log.Fatalf("prepare TLS certificate: %v", err)
	}

	relayServer := runtime.NewServer(cfg)
	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: relayServer.Routes(),
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		},
	}

	log.Printf("wsjtx-relay-server listening on https://%s", cfg.ListenAddr)
	log.Printf("shared secret loaded from %s", cfg.SharedSecretFile)
	if err := httpServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Fatalf("relay server stopped with error: %v", err)
	}
}
