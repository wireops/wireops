package routes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/registry"
	"github.com/wireops/wireops/internal/registryauth"
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
			// explicit `false` in draft mode.
			Insecure *bool `json:"insecure"`
		}
		e.Request.Body = http.MaxBytesReader(e.Response, e.Request.Body, maxRegistryCredentialTestBodyBytes)
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		savedName := ""
		if body.CredentialID != "" {
			// Persisted mode: probe exactly what is stored, so "the values I
			// typed work" can never mask "what was saved is broken" (the #186
			// failure mode). Any draft field alongside credential_id would
			// silently re-introduce the merge this check exists to prevent.
			if body.RegistryURL != "" || body.AuthType != "" || body.Username != "" || body.Password != "" || body.Insecure != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "when credential_id is set, registry_url, auth_type, username, password and insecure must be omitted — the saved credential is verified exactly as persisted; drop credential_id to test entered values"})
			}
			saved, err := registry.LoadCredentialByID(rr.app, body.CredentialID)
			if err != nil {
				return e.JSON(http.StatusOK, map[string]any{"success": false, "source": "persisted", "error": err.Error()})
			}
			savedName = saved.Name
			body.AuthType = string(saved.AuthType)
			body.Username = saved.Username
			body.Password = saved.Password
			body.RegistryURL = saved.RegistryURL
			body.Insecure = &saved.Insecure
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
		if body.CredentialID != "" {
			result["source"] = "persisted"
			result["credential_id"] = body.CredentialID
			result["credential_name"] = savedName
		} else {
			result["source"] = "draft"
		}

		if body.AuthType == "gcp_service_account" && !json.Valid([]byte(body.Password)) {
			result["warning"] = "Service account key is not valid JSON"
		}

		if err := testRegistryConnection(e.Request.Context(), host, body.Username, body.Password, insecure); err != nil {
			result["success"] = false
			result["error"] = err.Error()
			var probeErr *registryauth.ProbeError
			if errors.As(err, &probeErr) {
				result["diagnostic"] = probeErr.Fields()
			}
			return e.JSON(http.StatusOK, result)
		}
		result["success"] = true
		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}

// testRegistryConnection does a lightweight `GET <host>/v2/` handshake with
// HTTP Basic auth — the same check the worker itself runs before a pull
// (worker/executor/registry_auth.go: checkRegistryAuth) — so a bad
// credential is caught at save time, not at the next deploy. Failures are
// classified *registryauth.ProbeError by stage: DNS/dial inside the dial
// hook, TLS from the handshake, the rest from the response handling.
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
				return nil, &registryauth.ProbeError{Stage: registryauth.StageResolve, Category: registryauth.CategoryNetwork, Message: "invalid registry address"}
			}
			ip, err := resolveValidatedIP(dialCtx, dialHost)
			if err != nil {
				return nil, &registryauth.ProbeError{Stage: registryauth.StageResolve, Category: registryauth.CategoryNetwork, Message: "failed to resolve registry host"}
			}
			conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(dialCtx, network, net.JoinHostPort(ip.String(), port))
			if err != nil {
				return nil, &registryauth.ProbeError{Stage: registryauth.StageConnect, Category: registryauth.CategoryNetwork, Message: "could not connect to the registry host"}
			}
			return conn, nil
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
		return classifyRegistryTransportError(err)
	}
	defer resp.Body.Close()

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return registryauth.CheckResponse(ctx, client, resp, auth)
}

// classifyRegistryTransportError maps the error surface of client.Do (DNS and
// dial failures already come back as *registryauth.ProbeError from the dial
// hook; TLS handshake failures, timeouts and dead connections arrive here
// wrapped in *url.Error) onto a staged ProbeError. Messages carry no variable
// parts: certificate details, resolved IPs and dial targets stay out of the
// diagnostic, and credentials were never in the error path to begin with.
func classifyRegistryTransportError(err error) error {
	var probeErr *registryauth.ProbeError
	if errors.As(err, &probeErr) {
		return probeErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &registryauth.ProbeError{Stage: registryauth.StageConnect, Category: registryauth.CategoryNetwork, Message: "registry check timed out"}
	}
	var hostErr x509.HostnameError
	var certErr x509.CertificateInvalidError
	var authErr x509.UnknownAuthorityError
	if errors.As(err, &hostErr) || errors.As(err, &certErr) || errors.As(err, &authErr) || strings.Contains(err.Error(), "tls:") {
		return &registryauth.ProbeError{Stage: registryauth.StageTLS, Category: registryauth.CategoryTLS, Message: "TLS handshake with the registry failed (untrusted certificate or hostname mismatch)"}
	}
	return &registryauth.ProbeError{Stage: registryauth.StageConnect, Category: registryauth.CategoryNetwork, Message: "could not reach the registry host"}
}

func doRegistryTestRequest(ctx context.Context, client *http.Client, url, username, password string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(username, password)
	return client.Do(req)
}
