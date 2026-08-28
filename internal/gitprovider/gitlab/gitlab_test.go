package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wireops/wireops/internal/gitprovider"
)

func setEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GITLAB_OAUTH_CLIENT_SECRET", "client-secret")
	if baseURL != "" {
		t.Setenv("GITLAB_BASE_URL", baseURL)
	}
}

func TestSlugAndName(t *testing.T) {
	p := &Provider{}
	if p.Slug() != "gitlab" {
		t.Fatalf("expected slug %q, got %q", "gitlab", p.Slug())
	}
	if p.Name() != "GitLab" {
		t.Fatalf("expected name %q, got %q", "GitLab", p.Name())
	}
	if p.BasicAuthUsername() != "oauth2" {
		t.Fatalf("expected basic auth username %q, got %q", "oauth2", p.BasicAuthUsername())
	}
}

func TestConfigured(t *testing.T) {
	p := &Provider{}
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "")
	t.Setenv("GITLAB_OAUTH_CLIENT_SECRET", "")
	if p.Configured() {
		t.Fatal("expected Configured() false with no env vars set")
	}

	setEnv(t, "")
	if !p.Configured() {
		t.Fatal("expected Configured() true once client id/secret are set")
	}
}

func TestAuthorizeURLDefaultsToGitLabCom(t *testing.T) {
	setEnv(t, "")
	p := &Provider{}
	authURL := p.AuthorizeURL("state123", "https://wireops.example.com/api/custom/git-providers/gitlab/callback")
	if !strings.HasPrefix(authURL, "https://gitlab.com/oauth/authorize?") {
		t.Fatalf("expected gitlab.com authorize URL, got %q", authURL)
	}
	if !strings.Contains(authURL, "client_id=client-id") {
		t.Fatalf("expected client_id in authorize URL, got %q", authURL)
	}
	if !strings.Contains(authURL, "state=state123") {
		t.Fatalf("expected state in authorize URL, got %q", authURL)
	}
}

func TestAuthorizeURLUsesSelfHostedBaseURL(t *testing.T) {
	setEnv(t, "https://gitlab.example.com/")
	p := &Provider{}
	authURL := p.AuthorizeURL("state123", "https://wireops.example.com/callback")
	if !strings.HasPrefix(authURL, "https://gitlab.example.com/oauth/authorize?") {
		t.Fatalf("expected self-hosted authorize URL with trailing slash trimmed, got %q", authURL)
	}
}

// gitlabTestServer stands in for both a gitlab.com-shaped and a self-hosted
// GitLab instance: the provider only ever talks to config.GetGitLabBaseURL(),
// so pointing that at httptest.Server exercises the exact same code path a
// real self-hosted instance would.
func gitlabTestServer(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestExchangeCodeSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Fatalf("expected grant_type=authorization_code, got %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "the-code" {
			t.Fatalf("expected code=the-code, got %q", r.Form.Get("code"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-123",
			"refresh_token": "refresh-456",
			"token_type":    "Bearer",
			"expires_in":    7200,
		})
	})
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-123" {
			t.Fatalf("expected bearer token forwarded, got %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"username": "octocat-gl"})
	})
	server := gitlabTestServer(t, mux)
	setEnv(t, server.URL)

	p := &Provider{}
	token, err := p.ExchangeCode(context.Background(), "the-code", "https://wireops.example.com/callback")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if token.AccessToken != "access-123" {
		t.Errorf("expected access token %q, got %q", "access-123", token.AccessToken)
	}
	if token.RefreshToken != "refresh-456" {
		t.Errorf("expected refresh token %q, got %q", "refresh-456", token.RefreshToken)
	}
	if token.AccountLogin != "octocat-gl" {
		t.Errorf("expected account login %q, got %q", "octocat-gl", token.AccountLogin)
	}
	if token.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt for a token with expires_in set")
	}
}

func TestExchangeCodeProviderError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "code has expired",
		})
	})
	server := gitlabTestServer(t, mux)
	setEnv(t, server.URL)

	p := &Provider{}
	_, err := p.ExchangeCode(context.Background(), "stale-code", "https://wireops.example.com/callback")
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("expected invalid_grant error, got %v", err)
	}
}

func TestRefreshTokenSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Fatalf("expected grant_type=refresh_token, got %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "old-refresh" {
			t.Fatalf("expected refresh_token=old-refresh, got %q", r.Form.Get("refresh_token"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    7200,
		})
	})
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"username": "octocat-gl"})
	})
	server := gitlabTestServer(t, mux)
	setEnv(t, server.URL)

	p := &Provider{}
	token, err := p.RefreshToken(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if token.AccessToken != "new-access" || token.RefreshToken != "new-refresh" {
		t.Fatalf("unexpected refreshed token: %+v", token)
	}
}

func TestRefreshTokenEmptyReturnsNotSupported(t *testing.T) {
	p := &Provider{}
	_, err := p.RefreshToken(context.Background(), "")
	if err != gitprovider.ErrRefreshNotSupported {
		t.Fatalf("expected ErrRefreshNotSupported, got %v", err)
	}
}

func TestListOrganizations(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/groups", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"full_path": "my-group", "avatar_url": "https://gitlab.example.com/avatar.png"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	server := gitlabTestServer(t, mux)
	setEnv(t, server.URL)

	p := &Provider{}
	orgs, err := p.ListOrganizations(context.Background(), "token")
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if len(orgs) != 1 || orgs[0].Login != "my-group" {
		t.Fatalf("unexpected orgs: %+v", orgs)
	}
}

func TestListRepositoriesOwnAndGroup(t *testing.T) {
	var sawGroupPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"path_with_namespace": "me/my-project",
				"name":                "my-project",
				"visibility":          "private",
				"default_branch":      "main",
				"http_url_to_repo":    "https://gitlab.com/me/my-project.git",
				"namespace":           map[string]any{"full_path": "me"},
			},
		})
	})
	mux.HandleFunc("/api/v4/groups/my-group%2Fsubgroup/projects", func(w http.ResponseWriter, r *http.Request) {
		sawGroupPath = r.URL.Path
		if r.URL.Query().Get("page") != "1" {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"path_with_namespace": "my-group/subgroup/proj",
				"name":                "proj",
				"visibility":          "public",
				"default_branch":      "master",
				"http_url_to_repo":    "https://gitlab.com/my-group/subgroup/proj.git",
				"namespace":           map[string]any{"full_path": "my-group/subgroup"},
			},
		})
	})
	server := gitlabTestServer(t, mux)
	setEnv(t, server.URL)

	p := &Provider{}

	own, err := p.ListRepositories(context.Background(), "token", "")
	if err != nil {
		t.Fatalf("ListRepositories (own): %v", err)
	}
	if len(own) != 1 || own[0].FullName != "me/my-project" || !own[0].Private {
		t.Fatalf("unexpected own repos: %+v", own)
	}

	grouped, err := p.ListRepositories(context.Background(), "token", "my-group/subgroup")
	if err != nil {
		t.Fatalf("ListRepositories (group): %v", err)
	}
	if len(grouped) != 1 || grouped[0].FullName != "my-group/subgroup/proj" || grouped[0].Private {
		t.Fatalf("unexpected group repos: %+v", grouped)
	}
	if sawGroupPath == "" {
		t.Fatal("expected the URL-encoded group path handler to be hit")
	}
}

func TestListBranches(t *testing.T) {
	var sawPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/me%2Fmy-project/repository/branches", func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		if r.URL.Query().Get("page") != "1" {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "main"}, {"name": "dev"}})
	})
	server := gitlabTestServer(t, mux)
	setEnv(t, server.URL)

	p := &Provider{}
	branches, err := p.ListBranches(context.Background(), "token", "me/my-project")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %+v", branches)
	}
	if sawPath == "" {
		t.Fatal("expected the URL-encoded project path handler to be hit")
	}
}

func TestGetPropagatesHTTPErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"401 Unauthorized"}`)
	})
	server := gitlabTestServer(t, mux)
	setEnv(t, server.URL)

	p := &Provider{}
	_, err := p.fetchAuthenticatedUser(context.Background(), "bad-token")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected an error mentioning 401, got %v", err)
	}
}
