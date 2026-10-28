package registryauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBearerFlow(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		payload                     map[string]string
		tokenStatus, verifiedStatus int
		wantError                   bool
	}{
		{"token", map[string]string{"token": "valid-token"}, 200, 200, false},
		{"access-token", map[string]string{"access_token": "valid-token"}, 200, 200, false},
		{"rejected-credentials", map[string]string{"error": "secret-value-never-echoed"}, 401, 200, true},
		{"missing-token", map[string]string{}, 200, 200, true},
		{"rejected-token", map[string]string{"token": "valid-token"}, 200, 401, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var endpoint string
			tokens, verified := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/":
					if r.Header.Get("Authorization") == "Bearer valid-token" {
						verified++
						w.WriteHeader(tc.verifiedStatus)
						return
					}
					w.Header().Set("WWW-Authenticate", `Bearer realm="`+endpoint+`/auth/token",service="grom-registry",scope="repository:team/app:pull,push"`)
					w.WriteHeader(401)
				case "/auth/token":
					tokens++
					u, p, ok := r.BasicAuth()
					if !ok || u != "wireops" || p != "valid-key" {
						t.Error("missing basic credentials")
					}
					if r.URL.Query().Get("service") != "grom-registry" || r.URL.Query().Get("scope") != "repository:team/app:pull,push" {
						t.Error("challenge parameters lost")
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.tokenStatus)
					if err := json.NewEncoder(w).Encode(tc.payload); err != nil {
						t.Error(err)
					}
				}
			}))
			defer server.Close()
			endpoint = server.URL
			response, e := server.Client().Get(endpoint + "/v2/")
			if e != nil {
				t.Fatal(e)
			}
			defer response.Body.Close()
			e = CheckResponse(context.Background(), server.Client(), response, base64.StdEncoding.EncodeToString([]byte("wireops:valid-key")))
			if (e != nil) != tc.wantError {
				t.Fatalf("err=%v", e)
			}
			if e != nil && strings.Contains(e.Error(), "secret-value") {
				t.Fatal("response echoed into diagnostic")
			}
			if tokens != 1 {
				t.Fatal("token request count", tokens)
			}
			if !tc.wantError && verified != 1 {
				t.Fatal("token not verified")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBearerTokenResponseValidation(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"invalid-json", "not-json"},
		{"oversize", strings.Repeat("x", maxTokenResponse+1)},
		{"invalid-token-type", `{"token":123}`},
		{"token-with-newline", `{"token":"unsafe\r\ntoken"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe, err := http.NewRequest(http.MethodGet, "https://registry.example/v2/", nil)
			if err != nil {
				t.Fatal(err)
			}
			response := &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Www-Authenticate": []string{`Bearer realm="https://registry.example/auth"`}},
				Body:       io.NopCloser(strings.NewReader("")), Request: probe,
			}
			defer response.Body.Close()
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Path != "/auth" {
					t.Error("invalid token reached registry verification")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(tc.body)), Request: r,
				}, nil
			})}
			if err := CheckResponse(context.Background(), client, response, "sensitive-auth"); err == nil {
				t.Fatal("invalid token response accepted")
			}
			if requests != 1 {
				t.Fatalf("token request count = %d, want 1", requests)
			}
		})
	}
}

func TestBearerRealmPolicy(t *testing.T) {
	for _, tc := range []struct {
		probe, realm string
		trusted      bool
	}{
		{"https://registry.example/v2/", "https://registry.example/auth", true},
		{"http://registry.example/v2/", "http://registry.example/auth", true},
		{"https://registry.example/v2/", "http://registry.example/auth", false},
		{"https://registry.example/v2/", "https://attacker.example/auth", false},
		{"https://registry.example/v2/", "https://user:password@registry.example/auth", false},
		{"https://registry.example/v2/", "https://registry.example/auth#fragment", false},
		{"https://registry.example/v2/", "/auth", false},
		{"https://registry-1.docker.io/v2/", "https://auth.docker.io/token", true},
		{"https://registry-1.docker.io/v2/", "https://auth.docker.io/other", false},
		{"https://registry-1.docker.io/v2/", "https://attacker.example/token", false},
	} {
		a, _ := url.Parse(tc.probe)
		b, _ := url.Parse(tc.realm)
		if got := trustedRealm(a, b); got != tc.trusted {
			t.Errorf("%s -> %s = %v", tc.probe, tc.realm, got)
		}
	}
}
func TestBearerDoesNotFollowRedirectOrUntrustedRealm(t *testing.T) {
	leaked := 0
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked++; w.WriteHeader(200) }))
	defer attacker.Close()
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprint(redirect), func(t *testing.T) {
			var endpoint string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth" {
					http.Redirect(w, r, attacker.URL, http.StatusTemporaryRedirect)
					return
				}
				realm := attacker.URL
				if redirect {
					realm = endpoint + "/auth"
				}
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+realm+`"`)
				w.WriteHeader(401)
			}))
			defer server.Close()
			endpoint = server.URL
			response, e := server.Client().Get(endpoint + "/v2/")
			if e != nil {
				t.Fatal(e)
			}
			defer response.Body.Close()
			if e = CheckResponse(context.Background(), server.Client(), response, "sensitive-auth"); e == nil {
				t.Fatal("untrusted flow accepted")
			}
		})
	}
	if leaked != 0 {
		t.Fatal("credential destination was contacted")
	}
}
func TestBearerChallengeParsing(t *testing.T) {
	for _, tc := range []struct {
		header             string
		hasParams, invalid bool
	}{
		{`Basic realm="registry"`, false, false},
		{`Bearer realm="https://registry.example/token",service="a,b"`, true, false},
		{`bearer realm="https://registry.example/token"`, true, false},
		{`Bearer scope="pull"`, false, true},
		{`Bearer realm="unterminated`, false, true},
		{`Bearer realm="one",realm="two"`, false, true},
	} {
		p, e := bearerParams([]string{tc.header})
		if (p != nil) != tc.hasParams || (e != nil) != tc.invalid {
			t.Errorf("%s: %v %v", tc.header, p, e)
		}
	}
	for _, code := range []int{200, 404} {
		if e := checkStatus(code); e != nil {
			t.Fatal(e)
		}
	}
	for _, code := range []int{301, 401, 403, 500} {
		if e := checkStatus(code); e == nil {
			t.Fatal("status accepted", code)
		}
	}
}

func TestBearerInvalidRealmQuery(t *testing.T) {
	var endpoint string
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			tokenRequests++
			w.WriteHeader(200)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+endpoint+`/auth?invalid=%ZZ"`)
		w.WriteHeader(401)
	}))
	defer server.Close()
	endpoint = server.URL
	response, err := server.Client().Get(endpoint + "/v2/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err = CheckResponse(context.Background(), server.Client(), response, "sensitive-auth"); err == nil {
		t.Fatal("invalid realm query accepted")
	}
	if tokenRequests != 0 {
		t.Fatal("invalid query reached token issuer")
	}
}

// TestProbeErrorClassification pins the stage/category/http_status the API
// and MCP surfaces rely on to tell a challenge problem, a credential
// rejection and a permission failure apart — and that none of the structured
// fields can carry a token or token-service response body.
func TestProbeErrorClassification(t *testing.T) {
	challengeResponse := func(t *testing.T, header string) *http.Response {
		t.Helper()
		probe, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://registry.example/v2/", nil)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Www-Authenticate": []string{header}},
			Body:       io.NopCloser(strings.NewReader("")), Request: probe,
		}
	}

	t.Run("challenge-parse-failure", func(t *testing.T) {
		response := challengeResponse(t, `Bearer realm="unterminated`)
		defer response.Body.Close()
		err := CheckResponse(context.Background(), &http.Client{}, response, "sensitive-auth")
		var probeErr *ProbeError
		if !errors.As(err, &probeErr) {
			t.Fatalf("expected *ProbeError, got %T", err)
		}
		if probeErr.Stage != StageChallenge || probeErr.Category != CategoryAuthChallenge {
			t.Fatalf("unexpected classification: %+v", probeErr)
		}
	})

	t.Run("token-rejected", func(t *testing.T) {
		response := challengeResponse(t, `Bearer realm="https://registry.example/auth"`)
		defer response.Body.Close()
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"error":"secret-body-never-echoed"}`)), Request: r,
			}, nil
		})}
		err := CheckResponse(context.Background(), client, response, "sensitive-auth")
		var probeErr *ProbeError
		if !errors.As(err, &probeErr) {
			t.Fatalf("expected *ProbeError, got %T", err)
		}
		if probeErr.Stage != StageToken || probeErr.Category != CategoryAuthRejected || probeErr.HTTPStatus != http.StatusUnauthorized {
			t.Fatalf("unexpected classification: %+v", probeErr)
		}
		fields, _ := json.Marshal(probeErr.Fields())
		if strings.Contains(string(fields), "secret-body") {
			t.Fatalf("diagnostic leaks token-service response body: %s", fields)
		}
	})

	t.Run("verify-permission-denied", func(t *testing.T) {
		response := challengeResponse(t, `Bearer realm="https://registry.example/auth"`)
		defer response.Body.Close()
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			status := http.StatusOK
			if calls == 2 {
				status = http.StatusForbidden
			}
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"token":"valid-token"}`)), Request: r,
			}, nil
		})}
		err := CheckResponse(context.Background(), client, response, "sensitive-auth")
		var probeErr *ProbeError
		if !errors.As(err, &probeErr) {
			t.Fatalf("expected *ProbeError, got %T", err)
		}
		if probeErr.Stage != StageVerify || probeErr.Category != CategoryPermission || probeErr.HTTPStatus != http.StatusForbidden {
			t.Fatalf("unexpected classification: %+v", probeErr)
		}
		if probeErr.Hint() == "" {
			t.Fatal("expected a next-check hint")
		}
	})

	t.Run("token-request-network-error", func(t *testing.T) {
		response := challengeResponse(t, `Bearer realm="https://registry.example/auth"`)
		defer response.Body.Close()
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		})}
		err := CheckResponse(context.Background(), client, response, "sensitive-auth")
		var probeErr *ProbeError
		if !errors.As(err, &probeErr) {
			t.Fatalf("expected *ProbeError, got %T", err)
		}
		if probeErr.Stage != StageToken || probeErr.Category != CategoryNetwork || probeErr.HTTPStatus != 0 {
			t.Fatalf("unexpected classification: %+v", probeErr)
		}
		if fields := probeErr.Fields(); fields["http_status"] != nil {
			t.Fatalf("http_status must be omitted when there is no response: %#v", fields)
		}
	})
}
