// Package github implements gitprovider.Provider for GitHub, using a
// classic OAuth App (not a GitHub App) — tokens don't expire and there is
// no refresh flow, unlike GitHub Apps or GitLab.
package github

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

// httpClient bounds all outbound requests to GitHub (token exchange + REST
// API) so a hung connection doesn't stall an OAuth callback or a repo/branch
// listing indefinitely.
var httpClient = &http.Client{Timeout: 15 * time.Second}

func init() {
	gitprovider.Register(&Provider{})
}

// Provider implements gitprovider.Provider for github.com using raw
// net/http + encoding/json — no SDK, matching internal/notify's style.
type Provider struct{}

const (
	apiBaseURL     = "https://api.github.com"
	authorizeURL   = "https://github.com/login/oauth/authorize"
	tokenURL       = "https://github.com/login/oauth/access_token"
	perPage        = 100
	maxListedPages = 5 // caps listings at 500 items, matching other paginated routes in this repo
)

func (p *Provider) Slug() string { return "github" }
func (p *Provider) Name() string { return "GitHub" }

func (p *Provider) Configured() bool {
	return config.GetGitHubOAuthClientID() != "" && config.GetGitHubOAuthClientSecret() != ""
}

func (p *Provider) BasicAuthUsername() string { return "x-access-token" }

func (p *Provider) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", config.GetGitHubOAuthClientID())
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "repo read:org")
	q.Set("state", state)
	return authorizeURL + "?" + q.Encode()
}

type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

type githubUser struct {
	Login string `json:"login"`
}

func (p *Provider) ExchangeCode(ctx context.Context, code, redirectURI string) (*gitprovider.Token, error) {
	form := url.Values{}
	form.Set("client_id", config.GetGitHubOAuthClientID())
	form.Set("client_secret", config.GetGitHubOAuthClientSecret())
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: exchange code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tokenResp accessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("github: decode token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("github: %s: %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("github: token exchange returned no access_token")
	}

	user, err := p.fetchAuthenticatedUser(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}

	return &gitprovider.Token{
		AccessToken:  tokenResp.AccessToken,
		AccountLogin: user.Login,
	}, nil
}

func (p *Provider) RefreshToken(_ context.Context, _ string) (*gitprovider.Token, error) {
	return nil, gitprovider.ErrRefreshNotSupported
}

func (p *Provider) fetchAuthenticatedUser(ctx context.Context, accessToken string) (*githubUser, error) {
	var user githubUser
	if err := p.get(ctx, accessToken, "/user", &user); err != nil {
		return nil, fmt.Errorf("github: fetch authenticated user: %w", err)
	}
	return &user, nil
}

type githubOrg struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

func (p *Provider) ListOrganizations(ctx context.Context, accessToken string) ([]gitprovider.Org, error) {
	var out []gitprovider.Org
	err := p.getPaginated(ctx, accessToken, "/user/orgs", func(page json.RawMessage) (int, error) {
		var orgs []githubOrg
		if err := json.Unmarshal(page, &orgs); err != nil {
			return 0, err
		}
		for _, o := range orgs {
			out = append(out, gitprovider.Org{Login: o.Login, AvatarURL: o.AvatarURL})
		}
		return len(orgs), nil
	})
	return out, err
}

type githubRepo struct {
	FullName      string `json:"full_name"`
	Name          string `json:"name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (p *Provider) ListRepositories(ctx context.Context, accessToken, org string) ([]gitprovider.Repo, error) {
	path := "/user/repos?affiliation=owner,collaborator&sort=updated"
	if org != "" {
		path = "/orgs/" + url.PathEscape(org) + "/repos?sort=updated"
	}

	var out []gitprovider.Repo
	err := p.getPaginated(ctx, accessToken, path, func(page json.RawMessage) (int, error) {
		var repos []githubRepo
		if err := json.Unmarshal(page, &repos); err != nil {
			return 0, err
		}
		for _, r := range repos {
			out = append(out, gitprovider.Repo{
				FullName:      r.FullName,
				Name:          r.Name,
				Owner:         r.Owner.Login,
				Private:       r.Private,
				DefaultBranch: r.DefaultBranch,
				CloneURL:      r.CloneURL,
			})
		}
		return len(repos), nil
	})
	return out, err
}

type githubBranch struct {
	Name string `json:"name"`
}

func (p *Provider) ListBranches(ctx context.Context, accessToken, repoFullName string) ([]gitprovider.Branch, error) {
	owner, name, ok := strings.Cut(repoFullName, "/")
	if !ok {
		return nil, fmt.Errorf("github: invalid repository full name %q", repoFullName)
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/branches"

	var out []gitprovider.Branch
	err := p.getPaginated(ctx, accessToken, path, func(page json.RawMessage) (int, error) {
		var branches []githubBranch
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

// get performs a single authenticated GET against the GitHub REST API and
// decodes the JSON response into out.
func (p *Provider) get(ctx context.Context, accessToken, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github: GET %s: %s: %s", path, resp.Status, string(body))
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
