package sync

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

// Connect establishes a WebSocket connection authenticated by worker token.
func Connect(serverURL, token string) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer

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

	log.Printf("[WORKER] Dialing WebSocket %s...", u.String())
	conn, resp, err := dialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("websocket dial failed: %w", err)
	}

	log.Printf("[WORKER] Completed WebSocket connection establishment.")
	return conn, nil
}
