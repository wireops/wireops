// Package gitprovider defines a provider-agnostic contract for OAuth-based
// git hosting integrations (GitHub today; Gitea/Forgejo/GitLab can register
// as additional implementations without touching callers of this package).
package gitprovider

import (
	"context"
	"errors"
	"time"
)

// Org is an organization/group the authenticated account belongs to.
type Org struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// Repo is a single repository as returned by a provider's listing API.
type Repo struct {
	FullName      string `json:"full_name"` // "owner/repo"
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"` // https clone URL, credential-free
}

// Branch is a single branch of a repository.
type Branch struct {
	Name string `json:"name"`
}

// Token is what ExchangeCode/RefreshToken return; only AccessToken (and,
// when present, RefreshToken) are persisted encrypted — the rest informs
// storage/UX decisions.
type Token struct {
	AccessToken  string
	RefreshToken string    // "" if the provider/app type doesn't support refresh
	ExpiresAt    time.Time // zero value = does not expire
	AccountLogin string    // e.g. GitHub username, for display ("connected as @foo")
}

// ErrRefreshNotSupported is returned by RefreshToken when the provider (or
// app type, e.g. a classic GitHub OAuth App) does not support refresh
// tokens — the caller should treat the existing access token as long-lived.
var ErrRefreshNotSupported = errors.New("gitprovider: token refresh not supported by this provider")

// Provider is the provider-agnostic contract implemented by each git
// hosting integration. GitHub is the first implementation; Gitea/Forgejo/
// GitLab add a new package each, registered via init(), with zero changes
// to the reconciler, routes, or credential store beyond what's already
// generic over the provider slug.
type Provider interface {
	Slug() string // "github"
	Name() string // "GitHub"

	// Configured reports whether this provider's client id/secret are set
	// via env vars; callers use this to hide "Connect" in the UI otherwise.
	Configured() bool

	AuthorizeURL(state, redirectURI string) string
	ExchangeCode(ctx context.Context, code, redirectURI string) (*Token, error)
	RefreshToken(ctx context.Context, refreshToken string) (*Token, error) // may return ErrRefreshNotSupported

	ListOrganizations(ctx context.Context, accessToken string) ([]Org, error)
	ListRepositories(ctx context.Context, accessToken, org string) ([]Repo, error) // org == "" => the authenticated user's own repos
	ListBranches(ctx context.Context, accessToken, repoFullName string) ([]Branch, error)

	// BasicAuthUsername is the fixed username go-git's basic-auth transport
	// expects paired with the OAuth token as password (GitHub: "x-access-token").
	BasicAuthUsername() string
}
