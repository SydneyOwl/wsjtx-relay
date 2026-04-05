package tlsutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

func EnsureCertificate(certFile string, keyFile string) (tls.Certificate, error) {
	if fileExists(certFile) && fileExists(keyFile) {
		certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("load TLS certificate: %w", err)
		}
		return certificate, nil
	}

	certificatePEM, privateKeyPEM, err := generateSelfSignedCertificate()
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certFile, certificatePEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write TLS certificate: %w", err)
	}
	if err := os.WriteFile(keyFile, privateKeyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write TLS key: %w", err)
	}

	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load generated TLS certificate: %w", err)
	}
	return certificate, nil
}

func generateSelfSignedCertificate() ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate RSA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial number: %w", err)
	}

	hostName, _ := os.Hostname()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "wsjtx-relay-server",
			Organization: []string{"wsjtx-relay"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	if hostName != "" && hostName != "localhost" {
		template.DNSNames = append(template.DNSNames, hostName)
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create self-signed certificate: %w", err)
	}

	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	return certificatePEM, privateKeyPEM, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
