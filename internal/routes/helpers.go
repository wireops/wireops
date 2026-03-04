package routes

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/jfxdev/wireops/internal/crypto"
	"github.com/jfxdev/wireops/internal/git"
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
	workspace := filepath.Join(app.DataDir(), "repositories")
	base := filepath.Join(workspace, repoID)
	composePath := stack.GetString("compose_path")
	if composePath != "" && composePath != "." {
		return filepath.Join(base, composePath)
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
	records, err := app.FindAllRecords("repository_keys",
		dbx.HashExp{"repository": repoID},
	)
	if err != nil || len(records) == 0 {
		return nil, fmt.Errorf("no credential found")
	}

	rec := records[0]
	authType := git.AuthType(rec.GetString("auth_type"))
	cred := &git.Credential{AuthType: authType}

	secretKey := []byte(os.Getenv("SECRET_KEY"))

	switch authType {
	case git.AuthTypeSSH:
		keyEnc := rec.GetString("ssh_private_key")
		if keyEnc != "" && len(secretKey) == 32 {
			if keyBytes, err := crypto.Decrypt(keyEnc, secretKey); err == nil {
				cred.SSHPrivateKey = keyBytes
			}
		}
		ppEnc := rec.GetString("ssh_passphrase")
		if ppEnc != "" && len(secretKey) == 32 {
			if ppBytes, err := crypto.Decrypt(ppEnc, secretKey); err == nil {
				cred.SSHPassphrase = ppBytes
			}
		}
		cred.SSHKnownHost = rec.GetString("ssh_known_host")

	case git.AuthTypeBasic:
		cred.GitUsername = rec.GetString("git_username")
		pwdEnc := rec.GetString("git_password")
		if pwdEnc != "" && len(secretKey) == 32 {
			if pwdBytes, err := crypto.Decrypt(pwdEnc, secretKey); err == nil {
				cred.GitPassword = string(pwdBytes)
			}
		}
	}

	return cred, nil
}
