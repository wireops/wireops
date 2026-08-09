package routes

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/registry"
)

func (rr routeRegistrar) registerRegistryCredentialRoutes() {
	rr.r.POST("/api/custom/registry-credentials/test", func(e *core.RequestEvent) error {
		var body struct {
			CredentialID string `json:"credential_id"`
			RegistryURL  string `json:"registry_url"`
			AuthType     string `json:"auth_type"`
			Username     string `json:"username"`
			Password     string `json:"password"`
			Insecure     bool   `json:"insecure"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		if body.CredentialID != "" {
			saved, err := registry.LoadCredentialByID(rr.app, body.CredentialID)
			if err != nil {
				return e.JSON(http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			}
			if body.AuthType == "" {
				body.AuthType = string(saved.AuthType)
			}
			if body.Username == "" {
				body.Username = saved.Username
			}
			if body.Password == "" {
				body.Password = saved.Password
			}
			if body.RegistryURL == "" {
				body.RegistryURL = saved.RegistryURL
			}
			if !body.Insecure {
				body.Insecure = saved.Insecure
			}
		}

		host := registry.NormalizeRegistryHost(body.RegistryURL)
		if host == "" {
			return e.JSON(http.StatusOK, map[string]any{"success": false, "error": "registry_url is required"})
		}

		result := map[string]any{}
		if body.AuthType == "gcp_service_account" && !json.Valid([]byte(body.Password)) {
			result["warning"] = "Service account key is not valid JSON"
		}

		if err := testRegistryConnection(host, body.Username, body.Password, body.Insecure); err != nil {
			result["success"] = false
			result["error"] = err.Error()
			return e.JSON(http.StatusOK, result)
		}
		result["success"] = true
		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}

// testRegistryConnection does a lightweight `GET <host>/v2/` handshake with
// HTTP Basic auth — the same check the worker itself runs before a pull
// (worker/executor/registry_auth.go: checkRegistryAuth) — so a bad
// credential is caught at save time, not at the next deploy.
func testRegistryConnection(host, username, password string, insecure bool) error {
	client := &http.Client{Timeout: 5 * time.Second}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	resp, err := doRegistryTestRequest(client, "https://"+host+"/v2/", username, password)
	if err != nil && insecure {
		resp, err = doRegistryTestRequest(client, "http://"+host+"/v2/", username, password)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNotFound:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("authentication rejected (HTTP %d)", resp.StatusCode)
	default:
		return fmt.Errorf("unexpected response (HTTP %d)", resp.StatusCode)
	}
}

func doRegistryTestRequest(client *http.Client, url, username, password string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(username, password)
	return client.Do(req)
}
