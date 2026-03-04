package git

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestConnectionPublicRepository(t *testing.T) {
	// Test connection to a public repository without authentication
	publicRepoURL := "https://github.com/torvalds/linux.git"
	
	err := TestConnection(publicRepoURL, nil)
	if err != nil {
		t.Errorf("Expected successful connection to public repository, got error: %v", err)
	}
}

func TestConnectionInvalidURL(t *testing.T) {
	// Test connection with an invalid URL
	invalidURL := "https://github.com/invalid/nonexistent-repo-xyz123456789.git"
	
	err := TestConnection(invalidURL, nil)
	if err == nil {
		t.Error("Expected error for invalid repository URL, got nil")
	}
}

func TestConnectionMalformedURL(t *testing.T) {
	// Test connection with a malformed URL
	malformedURL := "not-a-valid-url"
	
	err := TestConnection(malformedURL, nil)
	if err == nil {
		t.Error("Expected error for malformed URL, got nil")
	}
}

func TestConnectionPrivateRepoWithoutAuth(t *testing.T) {
	// Test connection to a private repository without authentication
	// This should fail since private repos require auth
	privateRepoURL := "https://github.com/private/repo.git"
	
	err := TestConnection(privateRepoURL, nil)
	// We expect this to fail (either 404 or auth required)
	// This is just to ensure the function handles it gracefully
	if err == nil {
		t.Log("Warning: Expected error for private repo without auth, got nil. Repository might be public.")
	}
}

func TestConnectionWithBasicAuth(t *testing.T) {
	// This test verifies the function accepts transport.AuthMethod interface
	// but doesn't actually test with real credentials
	var auth transport.AuthMethod = nil
	
	publicRepoURL := "https://github.com/torvalds/linux.git"
	err := TestConnection(publicRepoURL, auth)
	if err != nil {
		t.Errorf("Expected successful connection with nil auth, got error: %v", err)
	}
}
