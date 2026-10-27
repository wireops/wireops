package pb_migrations

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/hooks"
	"github.com/wireops/wireops/internal/logstream"
	"github.com/wireops/wireops/internal/registry"
)

func TestRegistryCredentialHTTPSecretPersistence(t *testing.T) {
	t.Setenv("SECRET_KEY", "12345678901234567890123456789012")
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if _, err := app.FindCollectionByNameOrId("registry_credentials"); err != nil {
		t.Fatal(err)
	}
	actors := core.NewAuthCollection("registry_actors")
	actors.Fields.Add(&core.SelectField{Name: "role", Values: []string{"operator", "admin", "viewer"}, MaxSelect: 1})
	if err := app.Save(actors); err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, role := range []string{"operator", "admin", "viewer", "superuser"} {
		collection := actors
		if role == "superuser" {
			collection, err = app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
			if err != nil {
				t.Fatal(err)
			}
		}
		actor := core.NewRecord(collection)
		actor.Set("email", role+"@registry.example")
		actor.Set("password", "example-fixture-password")
		if role != "superuser" {
			actor.Set("role", role)
		}
		if err := app.Save(actor); err != nil {
			t.Fatal(err)
		}
		tokens[role], err = actor.NewAuthToken()
		if err != nil {
			t.Fatal(err)
		}
	}
	hooks.Register(app, nil, nil, logstream.New())
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, role string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", tokens[role])
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		return response
	}
	const base = "/api/collections/registry_credentials/records"
	const initial = "grm_fixture_initial-value"
	const rotated = "grm_fixture_rotated-value"
	var id string
	checkStored := func(t *testing.T, expected string) {
		t.Helper()
		credential, err := registry.LoadCredentialByID(app, id)
		if err != nil {
			t.Fatal(err)
		}
		if credential.Password != expected {
			t.Fatal("saved registry password does not match submitted value")
		}
		record, err := app.FindRecordById("registry_credentials", id)
		if err != nil || !crypto.IsEncrypted(record.GetString("password"), []byte("12345678901234567890123456789012")) {
			t.Fatal("registry password is not encrypted at rest")
		}
	}
	checkResponse := func(t *testing.T, response *httptest.ResponseRecorder) {
		t.Helper()
		if strings.Contains(response.Body.String(), `"password"`) || strings.Contains(response.Body.String(), initial) || strings.Contains(response.Body.String(), rotated) {
			t.Fatal("registry response exposes password field")
		}
	}
	t.Run("OperatorCreatesSecret", func(t *testing.T) {
		response := request(http.MethodPost, base, "operator", map[string]any{
			"name": "Private", "registry_url": "https://registry.example", "auth_type": "basic", "username": "deployer", "password": initial,
		})
		if response.Code != http.StatusOK {
			t.Fatalf("create status=%d", response.Code)
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.ID == "" {
			t.Fatal("missing created credential ID")
		}
		id = body.ID
		checkResponse(t, response)
		checkStored(t, initial)
	})
	if id == "" || t.Failed() {
		return
	}
	t.Run("AdminRotatesSecret", func(t *testing.T) {
		response := request(http.MethodPatch, base+"/"+id, "admin", map[string]any{"password": rotated})
		if response.Code != http.StatusOK {
			t.Fatalf("update status=%d", response.Code)
		}
		checkResponse(t, response)
		checkStored(t, rotated)
	})
	t.Run("MetadataUpdatePreservesSecret", func(t *testing.T) {
		response := request(http.MethodPatch, base+"/"+id, "operator", map[string]any{"name": "Renamed"})
		if response.Code != http.StatusOK {
			t.Fatalf("metadata update status=%d", response.Code)
		}
		checkResponse(t, response)
		checkStored(t, rotated)
	})
	t.Run("ViewerCannotRotateSecret", func(t *testing.T) {
		response := request(http.MethodPatch, base+"/"+id, "viewer", map[string]any{"password": "grm_fixture_denied-value"})
		if response.Code < 400 {
			t.Fatalf("viewer write accepted: status=%d", response.Code)
		}
		checkStored(t, rotated)
	})
	for _, role := range []string{"operator", "admin", "viewer", "superuser"} {
		t.Run("ReadsHideSecretFor"+role, func(t *testing.T) {
			for _, path := range []string{base, base + "/" + id, base + "/" + id + "?fields=id,password"} {
				response := request(http.MethodGet, path, role, nil)
				if response.Code != http.StatusOK {
					t.Fatalf("read status=%d", response.Code)
				}
				checkResponse(t, response)
			}
		})
	}
}
