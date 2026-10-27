package registryauth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBearerFlow(t *testing.T) {
	for _, tc := range []struct {
		name, payload               string
		tokenStatus, verifiedStatus int
		wantError                   bool
	}{
		{"token", `{"token":"valid-token"}`, 200, 200, false},
		{"access-token", `{"access_token":"valid-token"}`, 200, 200, false},
		{"rejected-credentials", `{"error":"secret-value-never-echoed"}`, 401, 200, true},
		{"invalid-json", `not-json`, 200, 200, true},
		{"missing-token", `{}`, 200, 200, true},
		{"rejected-token", `{"token":"valid-token"}`, 200, 401, true},
		{"oversize", strings.Repeat("x", maxTokenResponse+1), 200, 200, true},
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
					w.WriteHeader(tc.tokenStatus)
					fmt.Fprint(w, tc.payload)
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
