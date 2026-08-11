package routes

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/registry"
)

// maxRegistryCredentialTestBodyBytes bounds the request body for the test
// endpoint. A GCP service-account JSON key is the largest realistic payload
// (a few KB); 1MB leaves generous headroom without being unbounded.
const maxRegistryCredentialTestBodyBytes = 1 << 20

func (rr routeRegistrar) registerRegistryCredentialRoutes() {
	rr.r.POST("/api/custom/registry-credentials/test", func(e *core.RequestEvent) error {
		var body struct {
			CredentialID string `json:"credential_id"`
			RegistryURL  string `json:"registry_url"`
			AuthType     string `json:"auth_type"`
			Username     string `json:"username"`
			Password     string `json:"password"`
			// Insecure is a pointer so omission can be distinguished from an
			// explicit `false` — otherwise merging with a saved credential
			// below would treat "explicitly disabling insecure" the same as
			// "field not provided" and silently re-enable it from the saved
			// value.
			Insecure *bool `json:"insecure"`
		}
		e.Request.Body = http.MaxBytesReader(e.Response, e.Request.Body, maxRegistryCredentialTestBodyBytes)
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		if body.CredentialID != "" {
			saved, err := registry.LoadCredentialByID(rr.app, body.CredentialID)
			if err != nil {
				return e.JSON(http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			}
			if body.AuthType == "" {
				body.AuthType = string(saved.AuthType)
			}
			if body.Username == "" {
				body.Username = saved.Username
			}
			if body.Password == "" {
				body.Password = saved.Password
			}
			if body.RegistryURL == "" {
				body.RegistryURL = saved.RegistryURL
			}
			if body.Insecure == nil {
				body.Insecure = &saved.Insecure
			}
		}
		insecure := body.Insecure != nil && *body.Insecure

		host := registry.NormalizeRegistryHost(body.RegistryURL)
		if host == "" {
			return e.JSON(http.StatusOK, map[string]any{"success": false, "error": "registry_url is required"})
		}
		// The resolved host feeds a server-initiated outbound request below —
		// guard against SSRF into the server's own private network before
		// dialing it (see validatePublicHost).
		if err := validatePublicHost(e.Request.Context(), host); err != nil {
			return e.JSON(http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		}

		result := map[string]any{}
		if body.AuthType == "gcp_service_account" && !json.Valid([]byte(body.Password)) {
			result["warning"] = "Service account key is not valid JSON"
		}

		if err := testRegistryConnection(e.Request.Context(), host, body.Username, body.Password, insecure); err != nil {
			result["success"] = false
			result["error"] = err.Error()
			return e.JSON(http.StatusOK, result)
		}
		result["success"] = true
		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}

// testRegistryConnection does a lightweight `GET <host>/v2/` handshake with
// HTTP Basic auth — the same check the worker itself runs before a pull
// (worker/executor/registry_auth.go: checkRegistryAuth) — so a bad
// credential is caught at save time, not at the next deploy.
func testRegistryConnection(ctx context.Context, host, username, password string, insecure bool) error {
	transport := &http.Transport{
		// host was already SSRF-validated by the caller (validatePublicHost),
		// but re-resolving here for the actual dial would reopen a DNS
		// rebinding window — an attacker-controlled DNS record could resolve
		// to a public IP at validation time and a private one moments later
		// at connect time. Instead, dial the literal IP validatePublicHost
		// already approved, while leaving addr's hostname intact so the TLS
		// handshake still sends the correct SNI/ServerName.
		DialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			dialHost, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ip, err := resolveValidatedIP(dialCtx, dialHost)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(dialCtx, network, net.JoinHostPort(ip.String(), port))
		},
	}
	if insecure {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
		// Redirects would otherwise reissue the request (and Basic Auth
		// credentials) against a new, unvalidated host without going through
		// the SSRF check above.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := doRegistryTestRequest(ctx, client, "https://"+host+"/v2/", username, password)
	if err != nil && insecure {
		resp, err = doRegistryTestRequest(ctx, client, "http://"+host+"/v2/", username, password)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNotFound:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("authentication rejected (HTTP %d)", resp.StatusCode)
	default:
		return fmt.Errorf("unexpected response (HTTP %d)", resp.StatusCode)
	}
}

func doRegistryTestRequest(ctx context.Context, client *http.Client, url, username, password string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(username, password)
	return client.Do(req)
}
