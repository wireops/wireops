package sync

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	wiretls "github.com/wireops/wireops/pkg/tls"
)

// Connect establishes a WebSocket connection authenticated by the worker token.
// TLS behaviour (e.g. skip-verify for self-signed certs) is controlled via the
// WORKER_TLS_SKIP_VERIFY environment variable, handled centrally by pkg/tls.
func Connect(serverURL, token string) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer

	if tlsCfg := wiretls.BuildClientTLSConfig(); tlsCfg != nil {
		log.Printf("[worker] custom TLS client config applied (WORKER_TLS_SKIP_VERIFY)")
		dialer.TLSClientConfig = tlsCfg
	}

	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	u.Scheme = scheme
	u.Path = "/worker/ws"

	headers := make(http.Header)
	headers.Set("X-Wireops-Worker-Token", strings.TrimSpace(token))

	log.Printf("[worker] websocket dialing url=%s", u.String())
	conn, resp, err := dialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("websocket dial failed: %w", err)
	}

	log.Printf("[worker] websocket connected")
	return conn, nil
}
