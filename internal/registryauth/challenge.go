// Package registryauth completes the registry-v2 Bearer challenge without
// bypassing the caller's TLS, DNS/SSRF, timeout or redirect policies.
package registryauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

const maxTokenResponse = 1 << 20

// CheckResponse validates a registry probe and, for a Bearer challenge, obtains
// a token and retries that same probe. The caller owns the initial response.
// basicAuth is the base64 user:password entry from Docker's auth configuration.
func CheckResponse(ctx context.Context, client *http.Client, response *http.Response, basicAuth string) error {
	if response.StatusCode != http.StatusUnauthorized {
		return checkStatus(response.StatusCode)
	}
	params, err := bearerParams(response.Header.Values("WWW-Authenticate"))
	if err != nil {
		return err
	}
	if params == nil {
		return checkStatus(response.StatusCode)
	}
	if response.Request == nil || response.Request.URL == nil {
		return fmt.Errorf("missing registry probe origin")
	}
	probe := response.Request.URL
	realm, err := url.Parse(params["realm"])
	if err != nil || !trustedRealm(probe, realm) {
		return fmt.Errorf("registry Bearer token realm is not trusted")
	}
	query := realm.Query()
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
		return fmt.Errorf("registry Bearer token request failed")
	}
	defer authResponse.Body.Close()
	if authResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("registry Bearer token authentication rejected (HTTP %d)", authResponse.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(authResponse.Body, maxTokenResponse+1))
	if err != nil || len(raw) > maxTokenResponse {
		return fmt.Errorf("registry Bearer token response exceeds limit or is unreadable")
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("invalid registry Bearer token response")
	}
	token := payload.Token
	if token == "" {
		token = payload.AccessToken
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return fmt.Errorf("registry Bearer token response has no valid token")
	}
	verified, err := request(ctx, &bounded, probe.String(), "Bearer "+token)
	if err != nil {
		return fmt.Errorf("registry Bearer verification request failed")
	}
	defer verified.Body.Close()
	return checkStatus(verified.StatusCode)
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
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("authentication rejected (HTTP %d)", code)
	default:
		return fmt.Errorf("unexpected response (HTTP %d)", code)
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
			return nil, fmt.Errorf("invalid registry Bearer challenge")
		}
		return params, nil
	}
	return nil, nil
}
