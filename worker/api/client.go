package api

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrRevoked is returned when the server explicitly rejects the worker as revoked (HTTP 403).
// The caller must treat this as a fatal, permanent condition and must not retry.
var ErrRevoked = errors.New("worker is revoked by the server")

// NewMTLSClient builds an HTTP client configured with the worker's certificates for mTLS.
func NewMTLSClient(pkiDir string) (*http.Client, error) {
	workerCertPath := filepath.Join(pkiDir, "worker.crt")
	workerKeyPath := filepath.Join(pkiDir, "worker.key")
	caCertPath := filepath.Join(pkiDir, "ca.crt")

	cert, err := tls.LoadX509KeyPair(workerCertPath, workerKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load worker certs: %w", err)
	}

	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load CA cert: %w", err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertPEM)

	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		// We override the ServerName to "server" to match the hardcoded SAN
		// in the server's generated certificate. This allows standard Go TLS
		// to securely verify the connection without throwing Hostname mismatches
		// when workers connect via dynamic IPs, completely removing the need for
		// the insecure InsecureSkipVerify: true flag.
		ServerName: "server",
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

// Register sends the worker's initial metadata to the server using the mTLS connection.
// Tags are sourced from the WIREOPS_WORKER_TAGS environment variable on the worker host and
// stored in-memory on the server for job routing purposes.
// Returns ErrRevoked if the server responds with 403 — callers must not retry in that case.
func Register(client *http.Client, serverURL, hostname, version string, tags []string) error {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"hostname":   hostname,
		"ip_address": "",
		"version":    version,
		"tags":       tags,
	})

	serverURL = strings.TrimSuffix(serverURL, "/")
	resp, err := client.Post(serverURL+"/worker/register", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return ErrRevoked
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[WORKER] Completed initial registration with server.")
	return nil
}

// RenewResponse holds the server response to a certificate renewal request.
type RenewResponse struct {
	WorkerCert string `json:"worker_cert"`
	CACert     string `json:"ca_cert"`
}

// Renew sends a CSR to the server's mTLS renewal endpoint and returns the
// newly signed certificate and CA cert.
func Renew(client *http.Client, serverURL string, csrPEM []byte) (*RenewResponse, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"csr": string(csrPEM),
	})

	serverURL = strings.TrimSuffix(serverURL, "/")
	resp, err := client.Post(serverURL+"/worker/renew", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("renewal request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, ErrRevoked
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("renewal failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result RenewResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode renewal response: %w", err)
	}

	log.Printf("[WORKER] Certificate renewal completed successfully.")
	return &result, nil
}
