// SOPS+age support (P1.5). Unlike the other providers in this package, SOPS
// does not resolve a single key/value reference — it decrypts a whole
// secrets.yaml file (found next to a stack's wireops.yaml) into a flat
// key/value map, which the caller overlays on top of the stack's regular
// env vars. See internal/sync/reconciler.go for the overlay/precedence
// logic and internal/hooks/pb_hooks.go for per-repository keypair
// generation.
package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"filippo.io/age"
	sopswrapper "github.com/jfxdev/sops-wrapper"
	"github.com/jfxdev/sops-wrapper/keychain/entities"
	"gopkg.in/yaml.v3"
)

// decryptMu serializes SOPS decrypt calls. The underlying sops library reads
// the age identity from the SOPS_AGE_KEY_FILE process environment variable
// rather than accepting it as a call parameter, so concurrent decrypts for
// two repositories with different age keys would race on that env var
// without this lock. Decrypts are infrequent (once per reconcile, only when
// a secrets.yaml is present) so serializing them is not a throughput
// concern.
var decryptMu sync.Mutex

// GenerateAgeKeypair creates a new X25519 age identity and returns its
// private key ("AGE-SECRET-KEY-1...", to be encrypted at rest) and public
// recipient ("age1...", safe to display so operators can
// `sops -e --age <public key> secrets.yaml`).
func GenerateAgeKeypair() (privateKey, publicKey string, err error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", fmt.Errorf("sops: failed to generate age identity: %w", err)
	}
	return identity.String(), identity.Recipient().String(), nil
}

// DecryptSecretsFile decrypts a SOPS-encrypted YAML document (the contents
// of a secrets.yaml file) using the given age private key and returns its
// contents as a flat string map. Nested maps, sequences, and non-scalar
// values are rejected — secrets.yaml is meant to be a flat KEY: value list
// of env vars, not an arbitrary document.
func DecryptSecretsFile(ctx context.Context, content []byte, ageKeyPlaintext string) (map[string]string, error) {
	if ageKeyPlaintext == "" {
		return nil, fmt.Errorf("sops: repository has no age key configured")
	}

	decryptMu.Lock()
	defer decryptMu.Unlock()

	keyFile, err := os.CreateTemp("", "wireops-sops-age-*.key")
	if err != nil {
		return nil, fmt.Errorf("sops: failed to create temp age key file: %w", err)
	}
	keyPath := keyFile.Name()
	defer func() {
		_ = os.Remove(keyPath)
	}()

	if err := keyFile.Chmod(0o600); err != nil {
		_ = keyFile.Close()
		return nil, fmt.Errorf("sops: failed to set permissions on temp age key file: %w", err)
	}
	if _, err := keyFile.WriteString(ageKeyPlaintext); err != nil {
		_ = keyFile.Close()
		return nil, fmt.Errorf("sops: failed to write temp age key file: %w", err)
	}
	if err := keyFile.Close(); err != nil {
		return nil, fmt.Errorf("sops: failed to close temp age key file: %w", err)
	}

	prevKeyFile, hadPrevKeyFile := os.LookupEnv("SOPS_AGE_KEY_FILE")
	if err := os.Setenv("SOPS_AGE_KEY_FILE", keyPath); err != nil {
		return nil, fmt.Errorf("sops: failed to set SOPS_AGE_KEY_FILE: %w", err)
	}
	defer func() {
		if hadPrevKeyFile {
			_ = os.Setenv("SOPS_AGE_KEY_FILE", prevKeyFile)
		} else {
			_ = os.Unsetenv("SOPS_AGE_KEY_FILE")
		}
	}()

	cipher := sopswrapper.NewCipher()
	plaintext, err := cipher.Decrypt(ctx, content, sopswrapper.FormatYAML)
	if err != nil {
		return nil, fmt.Errorf("sops: failed to decrypt secrets file: %w", err)
	}

	return flattenSecretsYAML(plaintext)
}

// envKeyPattern mirrors the shape a value must have to be usable as a shell
// env var name at deploy time — this is stricter than the DB-level
// stack_env_vars.key field (which only requires non-empty), because a
// secrets.yaml entry skips that validation layer entirely and goes straight
// into the rendered .env file.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EncryptSecretsMap serializes values as a flat "KEY: value" YAML document
// and encrypts it with the given age public key (recipient), producing
// SOPS-encrypted content in the same shape DecryptSecretsFile expects. It
// never touches a private key or the filesystem — pure in-memory transform,
// used by the UI's "encrypt for SOPS" flow so operators can generate a
// secrets.yaml without the sops CLI.
func EncryptSecretsMap(ctx context.Context, values map[string]string, agePublicKey string) ([]byte, error) {
	if agePublicKey == "" {
		return nil, fmt.Errorf("sops: age public key is required")
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("sops: at least one key/value pair is required")
	}
	for key := range values {
		if !envKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("sops: %q is not a valid env var name (expected [A-Za-z_][A-Za-z0-9_]*)", key)
		}
	}

	plaintext, err := yaml.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("sops: failed to serialize secrets to YAML: %w", err)
	}

	cipher := sopswrapper.NewCipher()
	encrypted, err := cipher.Encrypt(ctx, plaintext, sopswrapper.EncryptionConfig{
		Format: sopswrapper.FormatYAML,
		Keys: []entities.EncryptionKey{
			{Platform: "age", ID: agePublicKey},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sops: failed to encrypt secrets: %w", err)
	}
	return encrypted, nil
}

// flattenSecretsYAML unmarshals decrypted YAML into a flat string map,
// rejecting any nested/non-scalar value with an actionable error.
func flattenSecretsYAML(plaintext []byte) (map[string]string, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(plaintext, &raw); err != nil {
		return nil, fmt.Errorf("sops: decrypted secrets file is not valid YAML: %w", err)
	}

	values := make(map[string]string, len(raw))
	for key, value := range raw {
		switch v := value.(type) {
		case string:
			values[key] = v
		case int, int64, float64, bool:
			values[key] = fmt.Sprintf("%v", v)
		case nil:
			values[key] = ""
		default:
			return nil, fmt.Errorf("sops: secrets.yaml must be a flat map of KEY: value pairs, but %q is not a scalar value", key)
		}
	}
	return values, nil
}

// SecretsFileNames are the accepted basenames for a SOPS-encrypted env file,
// checked in the same directory as wireops.yaml (i.e. the stack's resolved
// compose_path / workDir).
var SecretsFileNames = []string{"secrets.yaml", "secrets.yml"}

// FindSecretsFile returns the absolute path to a secrets.yaml/secrets.yml
// in dir, or "" if none exists. dir is the checked-out contents of a
// third-party git repository, so it is untrusted: a malicious commit could
// plant "secrets.yaml" as a symlink to an arbitrary host path (e.g.
// /etc/shadow) to have it read and echoed back through the decrypt-error
// response. Lstat (rather than Stat) deliberately does not follow symlinks,
// so a symlinked "secrets.yaml" is rejected instead of silently resolved.
func FindSecretsFile(dir string) (string, error) {
	for _, name := range SecretsFileNames {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("sops: failed to stat %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("sops: %q must be a regular file, not a symlink", path)
		}
		if info.IsDir() {
			continue
		}
		return path, nil
	}
	return "", nil
}
