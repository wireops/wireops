// Package tls — server.go
// Configures inbound TLS for the wireops server's worker port (:8443).
// This file is only used by the server binary; the worker binary never
// imports or calls anything defined here.
package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	certFileName = "tls-cert.pem"
	keyFileName  = "tls-key.pem"
)

// BuildServerTLSConfig returns a *tls.Config ready to be assigned to
// http.Server.TLSConfig, or nil when TLS_ENABLED is not "true".
//
// Environment variables:
//   - TLS_ENABLED   — set to "true" to activate TLS on the worker server port.
//   - TLS_CERT_FILE — path to a PEM-encoded certificate file.
//   - TLS_KEY_FILE  — path to a PEM-encoded private-key file.
//   - TLS_DATA_DIR  — directory where the auto-generated cert/key pair is
//     persisted (tls-cert.pem + tls-key.pem). Reused on restart so workers
//     keep trusting the same certificate. Ignored when TLS_CERT_FILE /
//     TLS_KEY_FILE are provided.
//
// When TLS_ENABLED=true but TLS_CERT_FILE / TLS_KEY_FILE are empty, a
// self-signed ECDSA P-256 certificate is used. Workers must then set
// WORKER_TLS_SKIP_VERIFY=true.
func BuildServerTLSConfig() (*tls.Config, error) {
	if os.Getenv("TLS_ENABLED") != "true" {
		return nil, nil
	}

	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")

	var cert tls.Certificate
	var err error

	if certFile != "" && keyFile != "" {
		cert, err = tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("loading TLS key pair: %w", err)
		}
	} else {
		cert, err = loadOrGenerateSelfSigned(os.Getenv("TLS_DATA_DIR"))
		if err != nil {
			return nil, fmt.Errorf("preparing self-signed certificate: %w", err)
		}
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// loadOrGenerateSelfSigned returns a self-signed certificate.
//
// When dataDir is non-empty the pair is persisted to disk and reloaded on
// subsequent calls. When dataDir is empty a new pair is generated in memory
// on every call (discarded when the process exits).
func loadOrGenerateSelfSigned(dataDir string) (tls.Certificate, error) {
	if dataDir != "" {
		certPath := filepath.Join(dataDir, certFileName)
		keyPath := filepath.Join(dataDir, keyFileName)

		if _, err := os.Stat(certPath); err == nil {
			cert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err == nil {
				return cert, nil
			}
			// Corrupted files — fall through and regenerate.
		}

		certPEM, keyPEM, err := generateSelfSignedPEM()
		if err != nil {
			return tls.Certificate{}, err
		}
		if err := os.MkdirAll(dataDir, 0700); err != nil {
			return tls.Certificate{}, fmt.Errorf("creating TLS_DATA_DIR %q: %w", dataDir, err)
		}
		if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
			return tls.Certificate{}, fmt.Errorf("writing %s: %w", certPath, err)
		}
		if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
			return tls.Certificate{}, fmt.Errorf("writing %s: %w", keyPath, err)
		}
		return tls.X509KeyPair(certPEM, keyPEM)
	}

	certPEM, keyPEM, err := generateSelfSignedPEM()
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// generateSelfSignedPEM creates an ECDSA P-256 self-signed certificate valid
// for 10 years and returns the PEM-encoded certificate and private key.
func generateSelfSignedPEM() (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"wireops self-signed"},
			CommonName:   "wireops-worker-server",
		},
		NotBefore:             time.Now().Add(-1 * time.Minute),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	return certPEM, keyPEM, nil
}
