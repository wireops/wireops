package pki

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
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
)

type BootstrapResponse struct {
	AgentID   string `json:"agent_id"`
	AgentCert string `json:"agent_cert"`
	CACert    string `json:"ca_cert"`
}

// Bootstrap connects to the main server with a seat token and exchanges an RSA CSR for signed certificates.
func Bootstrap(serverURL, token, pkiDir string) error {
	log.Printf("[AGENT] Starting certificate exchange with server...")

	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	template := x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{"wireops Agent"},
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
	resp, err := http.Post(serverURL+"/api/custom/agent/bootstrap", "application/json", bytes.NewBuffer(reqBody))
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

	if err := writeSafe(filepath.Join(pkiDir, "agent.key"), privPEM, 0600); err != nil {
		return err
	}
	if err := writeSafe(filepath.Join(pkiDir, "agent.crt"), []byte(result.AgentCert), 0644); err != nil {
		return err
	}
	if err := writeSafe(filepath.Join(pkiDir, "ca.crt"), []byte(result.CACert), 0644); err != nil {
		return err
	}

	log.Printf("[AGENT] Completed certificate exchange. Agent ID: %s", result.AgentID)
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

// HasValidCerts checks if the certificates already exist.
func HasValidCerts(pkiDir string) bool {
	paths := []string{
		filepath.Join(pkiDir, "agent.key"),
		filepath.Join(pkiDir, "agent.crt"),
		filepath.Join(pkiDir, "ca.crt"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// PurgeCredentials securely deletes the local PKI files left by a previous bootstrap.
// The private key is overwritten with zeros before removal to reduce forensic recoverability.
// Call this when the agent receives a permanent revocation from the server.
func PurgeCredentials(pkiDir string) {
	files := []string{
		filepath.Join(pkiDir, "agent.key"),
		filepath.Join(pkiDir, "agent.crt"),
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
			log.Printf("[AGENT] Warning: could not remove %s: %v", path, removeErr)
		} else {
			log.Printf("[AGENT] Removed credential file: %s", path)
		}
	}
}
