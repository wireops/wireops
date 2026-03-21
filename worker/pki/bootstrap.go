package pki

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BootstrapResponse struct {
	WorkerID   string `json:"worker_id"`
	WorkerCert string `json:"worker_cert"`
	CACert     string `json:"ca_cert"`
}

// Bootstrap connects to the main server with a seat token and exchanges an RSA CSR for signed certificates.
func Bootstrap(serverURL, token, pkiDir string) error {
	log.Printf("[WORKER] Starting certificate exchange with server...")

	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	template := x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{"wireops Worker"},
		},
		SignatureAlgorithm: x509.SHA256WithRSA,
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		return fmt.Errorf("failed to create CSR: %w", err)
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})

	reqBody, _ := json.Marshal(map[string]string{
		"bootstrap_token": token,
		"csr":             string(csrPEM),
	})

	serverURL = strings.TrimSuffix(serverURL, "/")
	resp, err := http.Post(serverURL+"/api/custom/worker/bootstrap", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("bootstrap request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bootstrap failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result BootstrapResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode bootstrap response: %w", err)
	}

	if err := os.MkdirAll(pkiDir, 0700); err != nil {
		return fmt.Errorf("failed to create PKI directory: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	if err := WriteCertificates(pkiDir, []byte(result.WorkerCert), privPEM, []byte(result.CACert)); err != nil {
		return err
	}

	log.Printf("[WORKER] Completed certificate exchange. Worker ID: %s", result.WorkerID)
	// IMPORTANT: certificate paths are NEVER logged here for security!
	return nil
}

func writeSafe(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// HasValidCerts reports whether all certificate files exist and are parseable.
// File existence alone is insufficient; corrupt or mismatched certs must also
// trigger re-bootstrapping.
func HasValidCerts(pkiDir string) bool {
	keyPath := filepath.Join(pkiDir, "worker.key")
	certPath := filepath.Join(pkiDir, "worker.crt")
	caPath := filepath.Join(pkiDir, "ca.crt")

	for _, p := range []string{keyPath, certPath, caPath} {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return false
		}
	}

	// Verify the keypair can be loaded.
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		return false
	}

	// Verify the CA cert can be parsed.
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(caPEM)
	if block == nil {
		return false
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return false
	}

	return true
}

// GetCertNotAfter parses the worker certificate and returns its expiry time.
func GetCertNotAfter(pkiDir string) (time.Time, error) {
	certPath := filepath.Join(pkiDir, "worker.crt")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read worker cert: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return time.Time{}, fmt.Errorf("failed to decode worker cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse worker cert: %w", err)
	}
	return cert.NotAfter, nil
}

// NeedsRenewal reports whether the worker certificate expires within thresholdDays.
// Any parse or validation error is treated as needing renewal (fail-closed).
func NeedsRenewal(pkiDir string, thresholdDays int) bool {
	notAfter, err := GetCertNotAfter(pkiDir)
	if err != nil {
		return true
	}
	return time.Until(notAfter) < time.Duration(thresholdDays)*24*time.Hour
}

// WriteCertificates atomically writes new cert material to disk.
// Each file is written to a .tmp path first, then renamed to prevent partial writes.
func WriteCertificates(pkiDir string, certPEM, keyPEM, caPEM []byte) error {
	if err := os.MkdirAll(pkiDir, 0700); err != nil {
		return fmt.Errorf("failed to create PKI directory: %w", err)
	}

	files := []struct {
		name string
		data []byte
		perm os.FileMode
	}{
		{"worker.key", keyPEM, 0600},
		{"worker.crt", certPEM, 0644},
		{"ca.crt", caPEM, 0644},
	}

	for _, f := range files {
		finalPath := filepath.Join(pkiDir, f.name)
		tmpPath := finalPath + ".tmp"
		if err := writeSafe(tmpPath, f.data, f.perm); err != nil {
			return fmt.Errorf("failed to write %s: %w", f.name, err)
		}
		if err := os.Rename(tmpPath, finalPath); err != nil {
			return fmt.Errorf("failed to rename %s: %w", f.name, err)
		}
	}
	return nil
}

// GenerateCSR creates a new RSA keypair and CSR for certificate renewal.
func GenerateCSR() (csrPEM []byte, keyPEM []byte, err error) {
	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}

	template := x509.CertificateRequest{
		Subject:            pkix.Name{Organization: []string{"wireops Worker"}},
		SignatureAlgorithm: x509.SHA256WithRSA,
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	return csrPEM, keyPEM, nil
}

// PurgeCredentials securely deletes the local PKI files left by a previous bootstrap.
// The private key is overwritten with zeros before removal to reduce forensic recoverability.
// Call this when the worker receives a permanent revocation from the server.
func PurgeCredentials(pkiDir string) {
	files := []string{
		filepath.Join(pkiDir, "worker.key"),
		filepath.Join(pkiDir, "worker.crt"),
		filepath.Join(pkiDir, "ca.crt"),
	}
	for _, path := range files {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		// Overwrite with zeros before removing (best-effort; not a guarantee on all filesystems).
		if err == nil {
			if f, openErr := os.OpenFile(path, os.O_WRONLY, 0); openErr == nil {
				zeros := make([]byte, info.Size())
				_, _ = f.Write(zeros)
				_ = f.Sync()
				f.Close()
			}
		}
		if removeErr := os.Remove(path); removeErr != nil {
			log.Printf("[WORKER] Warning: could not remove %s: %v", path, removeErr)
		} else {
			log.Printf("[WORKER] Removed credential file: %s", path)
		}
	}
}
