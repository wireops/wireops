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
)

// ErrRevoked is returned when the server explicitly rejects the agent as revoked (HTTP 403).
// The caller must treat this as a fatal, permanent condition and must not retry.
var ErrRevoked = errors.New("agent is revoked by the server")

// NewMTLSClient builds an HTTP client configured with the agent's certificates for mTLS.
func NewMTLSClient(pkiDir string) (*http.Client, error) {
	agentCertPath := filepath.Join(pkiDir, "agent.crt")
	agentKeyPath := filepath.Join(pkiDir, "agent.key")
	caCertPath := filepath.Join(pkiDir, "ca.crt")

	cert, err := tls.LoadX509KeyPair(agentCertPath, agentKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load agent certs: %w", err)
	}

	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load CA cert: %w", err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertPEM)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	return &http.Client{Transport: transport}, nil
}

// Register sends the agent's initial metadata to the server using the mTLS connection.
// Tags are sourced from the WIREOPS_AGENT_TAGS environment variable on the agent host and
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
	resp, err := client.Post(serverURL+"/agent/register", "application/json", bytes.NewBuffer(reqBody))
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

	log.Printf("[AGENT] Completed initial registration with server.")
	return nil
}
