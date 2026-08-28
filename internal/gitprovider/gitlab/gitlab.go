// Package gitlab implements gitprovider.Provider for GitLab — both
// gitlab.com and a self-hosted instance, selected by config.GetGitLabBaseURL()
// (GITLAB_BASE_URL, defaulting to https://gitlab.com). Unlike GitHub's
// classic OAuth App, GitLab OAuth applications issue a refresh token
// alongside a short-lived (2h by default) access token, so RefreshToken is a
// real implementation here rather than gitprovider.ErrRefreshNotSupported.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/gitprovider"
)

// httpClient bounds all outbound requests to GitLab (token exchange/refresh +
// REST API) so a hung connection doesn't stall an OAuth callback or a
// repo/branch listing indefinitely. CheckRedirect blocks an https->http
// downgrade mid-request (a malicious or hijacked redirect could otherwise
// make Go's client resend the Authorization/client_secret in plaintext);
// self-hosted instances deliberately configured for http throughout (see
// config.GetGitLabBaseURL) are unaffected since there's no https leg to
// downgrade from.
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("gitlab: stopped after 10 redirects")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("gitlab: refusing to follow redirect from https to %s", req.URL.Scheme)
		}
		return nil
	},
}

func init() {
	gitprovider.Register(&Provider{})
}

// Provider implements gitprovider.Provider for GitLab using raw net/http +
// encoding/json — no SDK, matching the GitHub provider's style.
type Provider struct{}

const (
	perPage        = 100
	maxListedPages = 5 // caps listings at 500 items, matching the GitHub provider
	// oauthScopes is the minimum needed to browse groups/projects/branches
	// (read_api) and to clone over HTTPS with the token as a basic-auth
	// password (read_repository) — never request write access.
	oauthScopes = "read_api read_repository"
)

func (p *Provider) Slug() string { return "gitlab" }
func (p *Provider) Name() string { return "GitLab" }

func (p *Provider) Configured() bool {
	return config.GetGitLabOAuthClientID() != "" && config.GetGitLabOAuthClientSecret() != ""
}

// BasicAuthUsername is "oauth2" by GitLab convention: any non-empty username
// paired with the OAuth token as password authenticates over HTTPS.
func (p *Provider) BasicAuthUsername() string { return "oauth2" }

func (p *Provider) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", config.GetGitLabOAuthClientID())
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", oauthScopes)
	q.Set("state", state)
	return config.GetGitLabBaseURL() + "/oauth/authorize?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

type gitlabUser struct {
	Username string `json:"username"`
}

func (p *Provider) ExchangeCode(ctx context.Context, code, redirectURI string) (*gitprovider.Token, error) {
	form := url.Values{}
	form.Set("client_id", config.GetGitLabOAuthClientID())
	form.Set("client_secret", config.GetGitLabOAuthClientSecret())
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURI)

	tokenResp, err := p.postToken(ctx, form)
	if err != nil {
		return nil, err
	}

	user, err := p.fetchAuthenticatedUser(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}

	token := &gitprovider.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		AccountLogin: user.Username,
	}
	if tokenResp.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	return token, nil
}

func (p *Provider) RefreshToken(ctx context.Context, refreshToken string) (*gitprovider.Token, error) {
	if refreshToken == "" {
		return nil, gitprovider.ErrRefreshNotSupported
	}

	form := url.Values{}
	form.Set("client_id", config.GetGitLabOAuthClientID())
	form.Set("client_secret", config.GetGitLabOAuthClientSecret())
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")

	tokenResp, err := p.postToken(ctx, form)
	if err != nil {
		return nil, err
	}

	user, err := p.fetchAuthenticatedUser(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}

	token := &gitprovider.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		AccountLogin: user.Username,
	}
	if tokenResp.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	return token, nil
}

func (p *Provider) postToken(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.GetGitLabBaseURL()+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("gitlab: decode token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("gitlab: %s: %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("gitlab: token request returned no access_token")
	}
	return &tokenResp, nil
}

func (p *Provider) fetchAuthenticatedUser(ctx context.Context, accessToken string) (*gitlabUser, error) {
	var user gitlabUser
	if err := p.get(ctx, accessToken, "/user", &user); err != nil {
		return nil, fmt.Errorf("gitlab: fetch authenticated user: %w", err)
	}
	return &user, nil
}

type gitlabGroup struct {
	FullPath  string `json:"full_path"`
	AvatarURL string `json:"avatar_url"`
}

func (p *Provider) ListOrganizations(ctx context.Context, accessToken string) ([]gitprovider.Org, error) {
	var out []gitprovider.Org
	err := p.getPaginated(ctx, accessToken, "/groups?min_access_level=10", func(page json.RawMessage) (int, error) {
		var groups []gitlabGroup
		if err := json.Unmarshal(page, &groups); err != nil {
			return 0, err
		}
		for _, g := range groups {
			out = append(out, gitprovider.Org{Login: g.FullPath, AvatarURL: g.AvatarURL})
		}
		return len(groups), nil
	})
	return out, err
}

type gitlabProject struct {
	PathWithNamespace string `json:"path_with_namespace"`
	Name              string `json:"name"`
	Visibility        string `json:"visibility"`
	DefaultBranch     string `json:"default_branch"`
	HTTPURLToRepo     string `json:"http_url_to_repo"`
	Namespace         struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
}

func (p *Provider) ListRepositories(ctx context.Context, accessToken, org string) ([]gitprovider.Repo, error) {
	path := "/projects?membership=true&order_by=last_activity_at"
	if org != "" {
		// GitLab's :id path parameter accepts a namespace's URL-encoded full
		// path (slashes as %2F) as an alternative to its numeric ID.
		path = "/groups/" + url.QueryEscape(org) + "/projects?order_by=last_activity_at"
	}

	var out []gitprovider.Repo
	err := p.getPaginated(ctx, accessToken, path, func(page json.RawMessage) (int, error) {
		var projects []gitlabProject
		if err := json.Unmarshal(page, &projects); err != nil {
			return 0, err
		}
		for _, proj := range projects {
			out = append(out, gitprovider.Repo{
				FullName:      proj.PathWithNamespace,
				Name:          proj.Name,
				Owner:         proj.Namespace.FullPath,
				Private:       proj.Visibility != "public",
				DefaultBranch: proj.DefaultBranch,
				CloneURL:      proj.HTTPURLToRepo,
			})
		}
		return len(projects), nil
	})
	return out, err
}

type gitlabBranch struct {
	Name string `json:"name"`
}

func (p *Provider) ListBranches(ctx context.Context, accessToken, repoFullName string) ([]gitprovider.Branch, error) {
	// Same URL-encoded-full-path convention as ListRepositories' group id.
	path := "/projects/" + url.QueryEscape(repoFullName) + "/repository/branches"

	var out []gitprovider.Branch
	err := p.getPaginated(ctx, accessToken, path, func(page json.RawMessage) (int, error) {
		var branches []gitlabBranch
		if err := json.Unmarshal(page, &branches); err != nil {
			return 0, err
		}
		for _, b := range branches {
			out = append(out, gitprovider.Branch{Name: b.Name})
		}
		return len(branches), nil
	})
	return out, err
}

// get performs a single authenticated GET against the GitLab REST v4 API and
// decodes the JSON response into out.
func (p *Provider) get(ctx context.Context, accessToken, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, config.GetGitLabBaseURL()+"/api/v4"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitlab: GET %s: %s: %s", path, resp.Status, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

// getPaginated walks page/per_page-numbered pages (capped at
// maxListedPages), calling decode with each page's raw JSON array; decode
// returns the number of items it found so getPaginated knows when to stop.
func (p *Provider) getPaginated(ctx context.Context, accessToken, path string, decode func(page json.RawMessage) (int, error)) error {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for page := 1; page <= maxListedPages; page++ {
		pagedPath := path + sep + "page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(perPage)

		var raw json.RawMessage
		if err := p.get(ctx, accessToken, pagedPath, &raw); err != nil {
			return err
		}

		count, err := decode(raw)
		if err != nil {
			return err
		}
		if count < perPage {
			break
		}
	}
	return nil
}
