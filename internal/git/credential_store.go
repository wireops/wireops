package git

import (
	"fmt"
	"os"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
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
	default:
		return nil, fmt.Errorf("unsupported repository key auth type %q", credential.AuthType)
	}

	return credential, nil
}
