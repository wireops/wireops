package routes

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/rbac"
)

func (rr routeRegistrar) registerCredentialRoutes() {
	rr.r.POST("/api/custom/credentials/test", func(e *core.RequestEvent) error {
		var body struct {
			RepositoryID  string `json:"repository_id"`
			RepositoryKey string `json:"repository_key_id"`
			GitURL        string `json:"git_url"`
			AuthType      string `json:"auth_type"`
			SSHKey        string `json:"ssh_private_key"`
			Passphrase    string `json:"ssh_passphrase"`
			KnownHost     string `json:"ssh_known_host"`
			GitUsername   string `json:"git_username"`
			GitPassword   string `json:"git_password"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		cred := git.Credential{
			AuthType:      git.AuthType(body.AuthType),
			SSHPrivateKey: []byte(body.SSHKey),
			SSHPassphrase: []byte(body.Passphrase),
			SSHKnownHost:  body.KnownHost,
			GitUsername:   body.GitUsername,
			GitPassword:   body.GitPassword,
		}

		if body.RepositoryID != "" || body.RepositoryKey != "" {
			var savedCred *git.Credential
			var err error
			if body.RepositoryKey != "" {
				savedCred, err = git.LoadCredentialByID(rr.app, body.RepositoryKey)
			} else {
				savedCred, err = loadRepositoryCredential(rr.app, body.RepositoryID)
			}
			if err != nil {
				log.Printf("TestConnection: failed to load credentials: %v", err)
				return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
			}
			if savedCred != nil {
				if cred.AuthType == git.AuthTypeNone || cred.AuthType == "" {
					cred.AuthType = savedCred.AuthType
				}
				if len(cred.SSHPrivateKey) == 0 && len(savedCred.SSHPrivateKey) > 0 {
					cred.SSHPrivateKey = savedCred.SSHPrivateKey
				}
				if len(cred.SSHPassphrase) == 0 && len(savedCred.SSHPassphrase) > 0 {
					cred.SSHPassphrase = savedCred.SSHPassphrase
				}
				if cred.SSHKnownHost == "" && savedCred.SSHKnownHost != "" {
					cred.SSHKnownHost = savedCred.SSHKnownHost
				}
				if cred.GitUsername == "" && savedCred.GitUsername != "" {
					cred.GitUsername = savedCred.GitUsername
				}
				if cred.GitPassword == "" && savedCred.GitPassword != "" {
					cred.GitPassword = savedCred.GitPassword
				}
			}
		}

		auth, err := git.ResolveTransportAuth(cred)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		if err := git.TestConnection(body.GitURL, auth); err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"success": "true"})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.POST("/api/custom/credentials/keyscan", func(e *core.RequestEvent) error {
		var body struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Host == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "host is required"})
		}

		var ips []net.IP
		if ip := net.ParseIP(body.Host); ip != nil {
			ips = append(ips, ip)
		} else {
			if !regexp.MustCompile(`^[a-zA-Z0-9.-]+$`).MatchString(body.Host) {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid host format"})
			}
			resolved, err := net.LookupIP(body.Host)
			if err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "failed to resolve host"})
			}
			ips = resolved
		}

		allowedRanges := os.Getenv("ALLOWED_PRIVATE_IP_RANGES")
		isIPAllowed := func(ip net.IP) bool {
			if allowedRanges == "" {
				return false
			}
			for _, part := range strings.Split(allowedRanges, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if _, ipNet, err := net.ParseCIDR(part); err == nil {
					if ipNet.Contains(ip) {
						return true
					}
				} else if parsedIP := net.ParseIP(part); parsedIP != nil && parsedIP.Equal(ip) {
					return true
				}
			}
			return false
		}

		for _, ip := range ips {
			if isIPAllowed(ip) {
				continue
			}
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
				return e.JSON(http.StatusForbidden, map[string]string{"error": "scanning private or loopback addresses is not allowed"})
			}
		}
		result, err := git.ScanHostKey(body.Host, body.Port)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"success": "true", "result": result})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}
