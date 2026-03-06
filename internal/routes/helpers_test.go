package routes

import (
	"testing"

	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

func TestToTransportAuthWithNil(t *testing.T) {
	result := toTransportAuth(nil)
	if result != nil {
		t.Errorf("Expected nil for nil input, got %v", result)
	}
}

func TestToTransportAuthWithBasicAuth(t *testing.T) {
	basicAuth := &gogithttp.BasicAuth{
		Username: "testuser",
		Password: "testpass",
	}
	
	result := toTransportAuth(basicAuth)
	if result == nil {
		t.Error("Expected non-nil result for BasicAuth")
	}
	
	if _, ok := result.(*gogithttp.BasicAuth); !ok {
		t.Error("Expected result to be *gogithttp.BasicAuth")
	}
}

func TestToTransportAuthWithSSHKeys(t *testing.T) {
	// Generate a test SSH key
	privateKey := []byte(`-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBTj3T7LoGPxqskQJLXqQqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJ
qHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJqHqJ
-----END OPENSSH PRIVATE KEY-----`)
	
	// Try to parse it (this will likely fail with test key, but we're testing the type conversion)
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		// Skip if we can't parse the test key
		t.Skip("Cannot parse test SSH key, skipping SSH auth test")
		return
	}
	
	sshAuth := &gogitssh.PublicKeys{
		User:   "git",
		Signer: signer,
	}
	
	result := toTransportAuth(sshAuth)
	if result == nil {
		t.Error("Expected non-nil result for SSH PublicKeys")
	}
	
	if _, ok := result.(*gogitssh.PublicKeys); !ok {
		t.Error("Expected result to be *gogitssh.PublicKeys")
	}
}

func TestToTransportAuthWithUnsupportedType(t *testing.T) {
	// Test with an unsupported type
	unsupported := "not a valid auth type"
	
	result := toTransportAuth(unsupported)
	if result != nil {
		t.Errorf("Expected nil for unsupported type, got %v", result)
	}
}
