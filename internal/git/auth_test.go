package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"

	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func TestResolveTransportAuthBasic(t *testing.T) {
	auth, err := ResolveTransportAuth(Credential{
		AuthType:    AuthTypeBasic,
		GitUsername: "octocat",
		GitPassword: "secret",
	})
	if err != nil {
		t.Fatalf("ResolveTransportAuth returned error: %v", err)
	}

	basicAuth, ok := auth.(*gogithttp.BasicAuth)
	if !ok {
		t.Fatalf("expected *http.BasicAuth, got %T", auth)
	}
	if basicAuth.Username != "octocat" || basicAuth.Password != "secret" {
		t.Fatalf("unexpected basic auth payload: %#v", basicAuth)
	}
}

func TestResolveTransportAuthSSH(t *testing.T) {
	auth, err := ResolveTransportAuth(Credential{
		AuthType:      AuthTypeSSH,
		SSHPrivateKey: generateOpenSSHPrivateKey(t),
	})
	if err != nil {
		t.Fatalf("ResolveTransportAuth returned error: %v", err)
	}

	if _, ok := auth.(*gogitssh.PublicKeys); !ok {
		t.Fatalf("expected *ssh.PublicKeys, got %T", auth)
	}
}

func TestResolveTransportAuthInvalidSSHKey(t *testing.T) {
	_, err := ResolveTransportAuth(Credential{
		AuthType:      AuthTypeSSH,
		SSHPrivateKey: []byte("not-a-private-key"),
	})
	if err == nil {
		t.Fatal("expected error for invalid SSH private key")
	}
	if !errors.Is(err, ErrInvalidSSHPrivateKey) {
		t.Fatalf("expected ErrInvalidSSHPrivateKey, got %v", err)
	}
}

func TestResolveTransportAuthMissingBasicUsername(t *testing.T) {
	_, err := ResolveTransportAuth(Credential{
		AuthType:    AuthTypeBasic,
		GitPassword: "secret",
	})
	if err == nil {
		t.Fatal("expected error for missing basic auth username")
	}
	if !errors.Is(err, ErrMissingGitUsername) {
		t.Fatalf("expected ErrMissingGitUsername, got %v", err)
	}
}

func generateOpenSSHPrivateKey(t *testing.T) []byte {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
}
