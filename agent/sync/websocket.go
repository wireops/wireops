package sync

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gorilla/websocket"
)

// Connect establishes an mTLS-secured WebSocket connection to the server.
func Connect(mtlsServerURL, pkiDir string) (*websocket.Conn, error) {
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
	if ok := caCertPool.AppendCertsFromPEM(caCertPEM); !ok {
		return nil, errors.New("failed to parse CA certificate PEM")
	}

	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		// We override the ServerName to "server" to match the hardcoded SAN 
		// in the server's generated certificate. This allows standard Go TLS
		// to securely verify the connection without throwing Hostname mismatches
		// when agents connect via dynamic IPs, completely removing the need for
		// the insecure InsecureSkipVerify: true flag.
		ServerName:   "server",
	}

	dialer := *websocket.DefaultDialer
	dialer.TLSClientConfig = tlsConfig

	u, err := url.Parse(mtlsServerURL)
	if err != nil {
		return nil, err
	}

	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws" // Only for purely local/insecure dev without TLS if configured
	}
	u.Scheme = scheme
	u.Path = "/agent/ws"

	log.Printf("[AGENT] Dialing WebSocket %s...", u.String())
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("websocket dial failed: %w", err)
	}

	log.Printf("[AGENT] Completed WebSocket connection establishment.")
	return conn, nil
}
