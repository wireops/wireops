package git

import (
	"fmt"
	"net"

	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type AuthType string

const (
	AuthTypeNone  AuthType = "none"
	AuthTypeSSH   AuthType = "ssh_key"
	AuthTypeBasic AuthType = "basic"
)

type Credential struct {
	AuthType       AuthType
	SSHPrivateKey  []byte
	SSHPassphrase  []byte
	SSHKnownHost   string
	GitUsername    string
	GitPassword    string
}

func ResolveAuth(cred Credential) (interface{}, error) {
	switch cred.AuthType {
	case AuthTypeSSH:
		return resolveSSHAuth(cred)
	case AuthTypeBasic:
		return resolveBasicAuth(cred)
	default:
		return nil, nil
	}
}

func resolveSSHAuth(cred Credential) (*gogitssh.PublicKeys, error) {
	var signer ssh.Signer
	var err error

	if len(cred.SSHPassphrase) > 0 {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(cred.SSHPrivateKey, cred.SSHPassphrase)
	} else {
		signer, err = ssh.ParsePrivateKey(cred.SSHPrivateKey)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH private key: %w", err)
	}

	var hostKeyCallback ssh.HostKeyCallback
	if cred.SSHKnownHost != "" {
		hostKeyCallback, err = buildKnownHostCallback(cred.SSHKnownHost)
		if err != nil {
			return nil, fmt.Errorf("failed to build known host callback: %w", err)
		}
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	return &gogitssh.PublicKeys{
		User:   "git",
		Signer: signer,
		HostKeyCallbackHelper: gogitssh.HostKeyCallbackHelper{
			HostKeyCallback: hostKeyCallback,
		},
	}, nil
}

func resolveBasicAuth(cred Credential) (*gogithttp.BasicAuth, error) {
	if cred.GitUsername == "" {
		return nil, fmt.Errorf("git username is required for basic auth")
	}
	return &gogithttp.BasicAuth{
		Username: cred.GitUsername,
		Password: cred.GitPassword,
	}, nil
}

func buildKnownHostCallback(knownHostEntry string) (ssh.HostKeyCallback, error) {
	f, err := knownhosts.New()
	if err != nil {
		return nil, err
	}
	_ = f
	return ssh.InsecureIgnoreHostKey(), nil
}

func ScanHostKey(host string, port int) (string, error) {
	if port == 0 {
		port = 22
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var hostKey ssh.PublicKey
	config := &ssh.ClientConfig{
		User: "git",
		Auth: []ssh.AuthMethod{},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			hostKey = key
			return nil
		},
	}

	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil && hostKey == nil {
		return "", fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	if conn != nil {
		conn.Close()
	}

	if hostKey == nil {
		return "", fmt.Errorf("no host key received from %s", addr)
	}

	fingerprint := ssh.FingerprintSHA256(hostKey)
	knownHostLine := knownhosts.Line([]string{host}, hostKey)
	return fmt.Sprintf("%s\n%s", fingerprint, knownHostLine), nil
}
