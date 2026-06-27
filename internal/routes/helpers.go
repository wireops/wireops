package routes

import (
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/safepath"
)

// stackWorkDir returns the working directory for docker compose operations.
// For local imported stacks (source_type=local) it uses filepath.Dir(import_path).
// For git-backed stacks it uses the cloned repository directory.
func stackWorkDir(app core.App, stack *core.Record) string {
	if stack.GetString("source_type") == "local" {
		if importPath := stack.GetString("import_path"); importPath != "" {
			return filepath.Dir(importPath)
		}
	}
	repoID := stack.GetString("repository")
	workspace := config.GetReposWorkspace()
	base := filepath.Join(workspace, repoID)
	composePath := stack.GetString("compose_path")
	if err := safepath.ValidateComposePath(composePath); err == nil && composePath != "" && composePath != "." {
		return filepath.Join(base, filepath.Clean(composePath))
	}
	return base
}

func toTransportAuth(auth interface{}) transport.AuthMethod {
	if auth == nil {
		return nil
	}
	switch v := auth.(type) {
	case *gogitssh.PublicKeys:
		return v
	case *gogithttp.BasicAuth:
		return v
	}
	return nil
}

func loadRepositoryCredential(app core.App, repoID string) (*git.Credential, error) {
	return git.LoadRepositoryCredential(app, repoID)
}
