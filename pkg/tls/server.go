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
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wireops/wireops/internal/crypto"
)

const (
	certFileName = "tls-cert.pem"
	keyFileName  = "tls-key.pem"
)

// BuildServerTLSConfig returns a *tls.Config ready to be assigned to
// http.Server.TLSConfig, or nil when TLS_ENABLED is not "true".
//
// Environment variables:
//   - TLS_ENABLED    — set to "true" to activate TLS on the worker server port.
//   - TLS_CERT_FILE  — path to a PEM-encoded certificate file.
//   - TLS_KEY_FILE   — path to a PEM-encoded private-key file.
//   - TLS_DATA_DIR   — directory where the auto-generated cert/key pair is
//     persisted (tls-cert.pem + tls-key.pem). Reused on restart so workers
//     keep trusting the same certificate. Ignored when TLS_CERT_FILE /
//     TLS_KEY_FILE are provided.
//   - SERVER_DOMAIN  — public hostname or IP address of this server added to
//     the auto-generated certificate's Subject Alternative Names. Required
//     when workers connect from a different host and WORKER_TLS_SKIP_VERIFY
//     is not set. Accepts a bare hostname (e.g. wireops.example.com) or an
//     IP address (e.g. 192.168.1.10). Ignored when TLS_CERT_FILE /
//     TLS_KEY_FILE are provided.
//
// When TLS_ENABLED=true but TLS_CERT_FILE / TLS_KEY_FILE are empty, a
// self-signed ECDSA P-256 certificate is used. Workers must then either set
// WORKER_TLS_SKIP_VERIFY=true or pin the generated certificate.
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
//
// The certificate (public) is stored as plain PEM. The private key is
// AES-GCM encrypted via internal/crypto before being written to disk,
// consistent with how wireops protects SSH keys and git passwords.
func loadOrGenerateSelfSigned(dataDir string) (tls.Certificate, error) {
	if dataDir != "" {
		certPath := filepath.Join(dataDir, certFileName)
		keyPath := filepath.Join(dataDir, keyFileName)

		// Try to load an existing pair. The key file holds encrypted ciphertext.
		if _, err := os.Stat(certPath); err == nil {
			certPEMOnDisk, err := os.ReadFile(certPath)
			if err == nil {
				keyPEM, err := loadAndDecryptKey(keyPath)
				if err == nil {
					cert, err := tls.X509KeyPair(certPEMOnDisk, keyPEM)
					if err == nil {
						return cert, nil
					}
				}
			}
			// Corrupted or undecryptable files — fall through and regenerate.
		}

		certPEM, keyPEM, err := generateSelfSignedPEM()
		if err != nil {
			return tls.Certificate{}, err
		}

		if err := os.MkdirAll(dataDir, 0700); err != nil {
			return tls.Certificate{}, fmt.Errorf("creating TLS_DATA_DIR %q: %w", dataDir, err)
		}

		// Write the certificate (public) as plain PEM.
		if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
			return tls.Certificate{}, fmt.Errorf("writing %s: %w", certPath, err)
		}

		// Encrypt and write the private key.
		if err := encryptAndWriteKey(keyPath, keyPEM); err != nil {
			return tls.Certificate{}, fmt.Errorf("writing encrypted key %s: %w", keyPath, err)
		}

		return tls.X509KeyPair(certPEM, keyPEM)
	}

	// No persistence directory — generate in memory, nothing touches disk.
	certPEM, keyPEM, err := generateSelfSignedPEM()
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// encryptAndWriteKey encrypts keyPEM with AES-GCM (using SECRET_KEY) and
// writes the base64-encoded ciphertext to path with mode 0600.
func encryptAndWriteKey(path string, keyPEM []byte) error {
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	if len(secretKey) != 32 {
		return fmt.Errorf("SECRET_KEY must be exactly 32 bytes to encrypt the TLS private key (got %d)", len(secretKey))
	}
	ciphertext, err := crypto.Encrypt(keyPEM, secretKey)
	if err != nil {
		return fmt.Errorf("encrypting TLS private key: %w", err)
	}
	return os.WriteFile(path, []byte(ciphertext), 0600)
}

// loadAndDecryptKey reads the encrypted key file at path, decrypts it with
// SECRET_KEY, and returns the plaintext PEM bytes (held only in memory).
func loadAndDecryptKey(path string) ([]byte, error) {
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	if len(secretKey) != 32 {
		return nil, fmt.Errorf("SECRET_KEY must be exactly 32 bytes to decrypt the TLS private key (got %d)", len(secretKey))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading encrypted key %s: %w", path, err)
	}
	plaintext, err := crypto.Decrypt(string(raw), secretKey)
	if err != nil {
		return nil, fmt.Errorf("decrypting TLS private key: %w", err)
	}
	return plaintext, nil
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

	// Collect SANs: localhost + system hostname (DNS) and loopback IPs.
	// Without SANs, modern TLS clients reject the certificate regardless of CN.
	dnsNames := []string{"localhost"}
	if h, err := os.Hostname(); err == nil && h != "" && h != "localhost" {
		dnsNames = append(dnsNames, h)
	}
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	// SERVER_DOMAIN: public hostname or IP of this server.
	// Required when workers run on a different host.
	if domain := strings.TrimSpace(os.Getenv("SERVER_DOMAIN")); domain != "" {
		if ip := net.ParseIP(domain); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		} else {
			dnsNames = append(dnsNames, domain)
		}
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"wireops self-signed"},
			CommonName:   "wireops-worker-server",
		},
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
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
