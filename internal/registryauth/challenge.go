// Package registryauth completes the registry-v2 Bearer challenge without
// bypassing the caller's TLS, DNS/SSRF, timeout or redirect policies.
package registryauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

const maxTokenResponse = 1 << 20

// Probe stages, in the order a registry check reaches them. "resolve",
// "connect" and "tls" are transport stages produced by the caller's dial
// setup; the rest classify the registry-v2 handshake itself.
const (
	StageResolve   = "resolve"   // DNS/host resolution
	StageConnect   = "connect"   // TCP dial
	StageTLS       = "tls"       // TLS handshake
	StageChallenge = "challenge" // initial /v2/ probe and its auth challenge
	StageToken     = "token"     // Bearer token exchange
	StageVerify    = "verify"    // authenticated re-probe with the token
)

// Stable categories for a failed probe. "auth_rejected" means the
// credentials themselves were refused; "permission" means the credentials
// were accepted but lack access — the distinction matters because the first
// blames the saved value while the second points at repository scope.
const (
	CategoryNetwork       = "network"
	CategoryTLS           = "tls"
	CategoryAuthChallenge = "auth_challenge"
	CategoryAuthRejected  = "auth_rejected"
	CategoryPermission    = "permission"
	CategoryUnexpected    = "unexpected"
)

// ProbeError is a classified registry probe failure. Message is a bounded,
// credential-free human explanation; the structured fields let an API or MCP
// client tell "the saved value is wrong" apart from "the network/TLS path is
// broken" or "the account lacks scope" without parsing prose.
type ProbeError struct {
	Stage      string `json:"stage"`
	Category   string `json:"category"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Message    string `json:"message"`
}

func (e *ProbeError) Error() string { return e.Message }

// Hint suggests the next check for the failure class. It deliberately avoids
// assuming an authentication failure means the operator typed an invalid key:
// only auth_rejected points at the saved credential.
func (e *ProbeError) Hint() string {
	switch {
	case e.Stage == StageResolve:
		return "check the registry hostname and DNS resolution from the WireOps server"
	case e.Stage == StageConnect:
		return "check that the registry host and port are reachable from the WireOps server (firewall, security groups)"
	case e.Category == CategoryTLS:
		return "check the registry's TLS certificate; for a self-signed or internal CA, either enable insecure on the credential (HTTPS registries only) or add the CA to the server trust store"
	case e.Category == CategoryAuthChallenge:
		return "check the registry's auth configuration — the Bearer challenge must point at a token service on the registry's own host"
	case e.Stage == StageToken && e.Category == CategoryAuthRejected:
		return "the token service rejected the credentials — verify the saved username/password or token"
	case e.Stage == StageVerify && e.Category == CategoryPermission:
		return "the registry accepted the credentials but denied access — check the account's repository scope and permissions"
	case e.Category == CategoryAuthRejected:
		return "the registry rejected the credentials — verify the saved username/password or token"
	default:
		return "check the registry server logs for the failing request"
	}
}

// Fields renders the error as the JSON shape exposed by API/MCP responses.
func (e *ProbeError) Fields() map[string]any {
	fields := map[string]any{
		"stage":    e.Stage,
		"category": e.Category,
		"message":  e.Message,
		"hint":     e.Hint(),
	}
	if e.HTTPStatus != 0 {
		fields["http_status"] = e.HTTPStatus
	}
	return fields
}

// stageErr stamps err with a probe stage when it is a ProbeError, so callers
// can reuse shared classifiers (checkStatus) without losing the stage.
func stageErr(stage string, err error) error {
	var probeErr *ProbeError
	if errors.As(err, &probeErr) {
		probeErr.Stage = stage
		return probeErr
	}
	return err
}

// CheckResponse validates a registry probe and, for a Bearer challenge, obtains
// a token and retries that same probe. The caller owns the initial response.
// basicAuth is the base64 user:password entry from Docker's auth configuration.
// Failures come back as *ProbeError classified by stage and category; the
// message strings are the ones the worker has always logged.
func CheckResponse(ctx context.Context, client *http.Client, response *http.Response, basicAuth string) error {
	if response.StatusCode != http.StatusUnauthorized {
		return stageErr(StageChallenge, checkStatus(response.StatusCode))
	}
	params, err := bearerParams(response.Header.Values("WWW-Authenticate"))
	if err != nil {
		return stageErr(StageChallenge, err)
	}
	if params == nil {
		return stageErr(StageChallenge, checkStatus(response.StatusCode))
	}
	if response.Request == nil || response.Request.URL == nil {
		return &ProbeError{Stage: StageChallenge, Category: CategoryUnexpected, Message: "missing registry probe origin"}
	}
	probe := response.Request.URL
	realm, err := url.Parse(params["realm"])
	if err != nil || !trustedRealm(probe, realm) {
		return &ProbeError{Stage: StageChallenge, Category: CategoryAuthChallenge, Message: "registry Bearer token realm is not trusted"}
	}
	query, err := url.ParseQuery(realm.RawQuery)
	if err != nil {
		return &ProbeError{Stage: StageChallenge, Category: CategoryAuthChallenge, Message: "invalid registry Bearer token realm query"}
	}
	for _, key := range []string{"service", "scope"} {
		if value := params[key]; value != "" {
			query.Set(key, value)
		}
	}
	realm.RawQuery = query.Encode()
	// Never follow redirects with Basic credentials or a registry token. Preserve
	// the supplied transport (including its per-dial DNS/SSRF checks).
	bounded := *client
	if !strings.EqualFold(probe.Host, realm.Host) {
		if transport, ok := client.Transport.(*http.Transport); ok {
			clone := transport.Clone()
			if clone.TLSClientConfig != nil {
				clone.TLSClientConfig = clone.TLSClientConfig.Clone()
				clone.TLSClientConfig.InsecureSkipVerify = false
			}
			bounded.Transport = clone
		}
	}
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	authResponse, err := request(ctx, &bounded, realm.String(), "Basic "+basicAuth)
	if err != nil {
		return &ProbeError{Stage: StageToken, Category: CategoryNetwork, Message: "registry Bearer token request failed"}
	}
	defer authResponse.Body.Close()
	if authResponse.StatusCode != http.StatusOK {
		return &ProbeError{Stage: StageToken, Category: CategoryAuthRejected, HTTPStatus: authResponse.StatusCode, Message: fmt.Sprintf("registry Bearer token authentication rejected (HTTP %d)", authResponse.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(authResponse.Body, maxTokenResponse+1))
	if err != nil || len(raw) > maxTokenResponse {
		return &ProbeError{Stage: StageToken, Category: CategoryUnexpected, Message: "registry Bearer token response exceeds limit or is unreadable"}
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return &ProbeError{Stage: StageToken, Category: CategoryUnexpected, Message: "invalid registry Bearer token response"}
	}
	token := payload.Token
	if token == "" {
		token = payload.AccessToken
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return &ProbeError{Stage: StageToken, Category: CategoryUnexpected, Message: "registry Bearer token response has no valid token"}
	}
	verified, err := request(ctx, &bounded, probe.String(), "Bearer "+token)
	if err != nil {
		return &ProbeError{Stage: StageVerify, Category: CategoryNetwork, Message: "registry Bearer verification request failed"}
	}
	defer verified.Body.Close()
	return stageErr(StageVerify, checkStatus(verified.StatusCode))
}
func request(ctx context.Context, client *http.Client, endpoint, authorization string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authorization)
	return client.Do(req)
}
func checkStatus(code int) error {
	switch code {
	case http.StatusOK, http.StatusNotFound:
		return nil
	case http.StatusUnauthorized:
		return &ProbeError{Category: CategoryAuthRejected, HTTPStatus: code, Message: fmt.Sprintf("authentication rejected (HTTP %d)", code)}
	case http.StatusForbidden:
		return &ProbeError{Category: CategoryPermission, HTTPStatus: code, Message: fmt.Sprintf("authentication rejected (HTTP %d)", code)}
	default:
		return &ProbeError{Category: CategoryUnexpected, HTTPStatus: code, Message: fmt.Sprintf("unexpected response (HTTP %d)", code)}
	}
}

// The configured registry must not redirect credentials to an arbitrary host.
// Docker Hub is the explicit exception: its documented token issuer has a
// different origin. Other registries use a same-origin realm (including Grom).
func trustedRealm(probe, realm *url.URL) bool {
	if probe == nil || realm == nil || realm.User != nil || realm.Fragment != "" || realm.Host == "" {
		return false
	}
	if realm.Scheme != "https" && !(probe.Scheme == "http" && realm.Scheme == "http") {
		return false
	}
	if strings.EqualFold(probe.Host, realm.Host) && probe.Scheme == realm.Scheme {
		return true
	}
	return probe.Scheme == "https" && strings.EqualFold(probe.Host, "registry-1.docker.io") && realm.Scheme == "https" && strings.EqualFold(realm.Host, "auth.docker.io") && realm.Path == "/token"
}
func bearerParams(headers []string) (map[string]string, error) {
	for _, header := range headers {
		scheme, rest, ok := strings.Cut(strings.TrimSpace(header), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			continue
		}
		// MIME parameter parsing handles quoted commas, escapes and duplicate keys;
		// only separators outside quoted values differ from HTTP auth parameters.
		var b strings.Builder
		quoted, escaped := false, false
		for _, c := range rest {
			if escaped {
				b.WriteRune(c)
				escaped = false
				continue
			}
			if quoted && c == '\\' {
				escaped = true
				b.WriteRune(c)
				continue
			}
			if c == '"' {
				quoted = !quoted
			}
			if c == ',' && !quoted {
				b.WriteRune(';')
			} else {
				b.WriteRune(c)
			}
		}
		_, params, err := mime.ParseMediaType("Bearer;" + b.String())
		if err != nil || params["realm"] == "" {
			return nil, &ProbeError{Category: CategoryAuthChallenge, Message: "invalid registry Bearer challenge"}
		}
		return params, nil
	}
	return nil, nil
}
