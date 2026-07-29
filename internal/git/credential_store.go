package git

import (
	"fmt"
	"os"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/gitprovider"
)

// LoadRepositoryCredential resolves the optional reusable key assigned to a repository.
func LoadRepositoryCredential(app core.App, repositoryID string) (*Credential, error) {
	repository, err := app.FindRecordById("repositories", repositoryID)
	if err != nil {
		return nil, fmt.Errorf("find repository: %w", err)
	}
	keyID := repository.GetString("repository_key")
	if keyID == "" {
		return &Credential{AuthType: AuthTypeNone}, nil
	}
	return LoadCredentialByID(app, keyID)
}

// LoadCredentialByID loads and decrypts a reusable repository key.
func LoadCredentialByID(app core.App, keyID string) (*Credential, error) {
	record, err := app.FindRecordById("repository_keys", keyID)
	if err != nil {
		return nil, fmt.Errorf("find repository key: %w", err)
	}

	credential := &Credential{AuthType: AuthType(record.GetString("auth_type"))}
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))

	decrypt := func(field string) ([]byte, error) {
		value := record.GetString(field)
		if value == "" {
			return nil, nil
		}
		if len(secretKey) != 32 {
			return nil, fmt.Errorf("SECRET_KEY must be exactly 32 bytes")
		}
		plain, err := crypto.Decrypt(value, secretKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt %s: %w", field, err)
		}
		return plain, nil
	}

	switch credential.AuthType {
	case AuthTypeSSH:
		if credential.SSHPrivateKey, err = decrypt("ssh_private_key"); err != nil {
			return nil, err
		}
		if credential.SSHPassphrase, err = decrypt("ssh_passphrase"); err != nil {
			return nil, err
		}
		credential.SSHKnownHost = record.GetString("ssh_known_host")
	case AuthTypeBasic:
		credential.GitUsername = record.GetString("git_username")
		password, decryptErr := decrypt("git_password")
		if decryptErr != nil {
			return nil, decryptErr
		}
		credential.GitPassword = string(password)
	case AuthTypeOAuthToken:
		providerSlug := record.GetString("oauth_provider")
		provider, ok := gitprovider.Get(providerSlug)
		if !ok {
			return nil, fmt.Errorf("unknown git provider %q", providerSlug)
		}
		token, decryptErr := decrypt("oauth_token")
		if decryptErr != nil {
			return nil, decryptErr
		}
		if len(token) == 0 {
			return nil, fmt.Errorf("repository key %q has no oauth_token stored", keyID)
		}
		// Downgrade to basic auth for go-git's transport: ResolveTransportAuth
		// already builds gogithttp.BasicAuth generically, so it needs no
		// changes to understand OAuth tokens.
		credential.AuthType = AuthTypeBasic
		credential.GitUsername = provider.BasicAuthUsername()
		credential.GitPassword = string(token)
	default:
		return nil, fmt.Errorf("unsupported repository key auth type %q", credential.AuthType)
	}

	return credential, nil
}

// LoadOAuthToken decrypts and returns the raw OAuth access token stored on a
// repository_keys record, along with the provider slug that issued it. Used
// by git provider discovery routes (list orgs/repos/branches), which need
// the token itself rather than a go-git Credential.
func LoadOAuthToken(app core.App, keyID string) (provider, token string, err error) {
	record, err := app.FindRecordById("repository_keys", keyID)
	if err != nil {
		return "", "", fmt.Errorf("find repository key: %w", err)
	}
	if AuthType(record.GetString("auth_type")) != AuthTypeOAuthToken {
		return "", "", fmt.Errorf("repository key %q is not an oauth_token credential", keyID)
	}

	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	if len(secretKey) != 32 {
		return "", "", fmt.Errorf("SECRET_KEY must be exactly 32 bytes")
	}

	value := record.GetString("oauth_token")
	if value == "" {
		return "", "", fmt.Errorf("repository key %q has no oauth_token stored", keyID)
	}
	plain, err := crypto.Decrypt(value, secretKey)
	if err != nil {
		return "", "", fmt.Errorf("decrypt oauth_token: %w", err)
	}

	return record.GetString("oauth_provider"), string(plain), nil
}
