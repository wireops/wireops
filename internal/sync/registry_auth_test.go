package sync

import (
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/crypto"
)

func newResolveRegistryAuthTestApp(t *testing.T) (*tests.TestApp, *core.Collection, *core.Collection) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	credentials := core.NewBaseCollection("registry_credentials")
	credentials.Fields.Add(&core.TextField{Name: "name"})
	credentials.Fields.Add(&core.TextField{Name: "registry_url"})
	credentials.Fields.Add(&core.TextField{Name: "auth_type"})
	credentials.Fields.Add(&core.TextField{Name: "username"})
	credentials.Fields.Add(&core.TextField{Name: "password"})
	credentials.Fields.Add(&core.BoolField{Name: "insecure"})
	if err := app.Save(credentials); err != nil {
		t.Fatalf("save registry_credentials collection: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.RelationField{Name: "registry_credential", CollectionId: credentials.Id, MaxSelect: 1})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("save stacks collection: %v", err)
	}

	return app, stacks, credentials
}

func TestResolveRegistryAuthWithoutCredential(t *testing.T) {
	app, stacks, _ := newResolveRegistryAuthTestApp(t)
	r := &Reconciler{app: app}

	stack := core.NewRecord(stacks)
	stack.Set("name", "no-credential")
	if err := app.Save(stack); err != nil {
		t.Fatalf("save stack: %v", err)
	}

	authB64, insecureHosts := r.resolveRegistryAuth(stack)
	if authB64 != "" || insecureHosts != nil {
		t.Fatalf("expected empty result for stack without a registry_credential, got authB64=%q insecureHosts=%v", authB64, insecureHosts)
	}
}

func TestResolveRegistryAuthWithCredential(t *testing.T) {
	app, stacks, credentials := newResolveRegistryAuthTestApp(t)
	r := &Reconciler{app: app}
	secret := "0123456789abcdef0123456789abcdef"
	t.Setenv("SECRET_KEY", secret)

	encrypted, err := crypto.Encrypt([]byte("hunter2"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	cred := core.NewRecord(credentials)
	cred.Set("name", "GHCR")
	cred.Set("registry_url", "https://ghcr.io")
	cred.Set("auth_type", "basic")
	cred.Set("username", "deploy")
	cred.Set("password", encrypted)
	cred.Set("insecure", true)
	if err := app.Save(cred); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	stack := core.NewRecord(stacks)
	stack.Set("name", "with-credential")
	stack.Set("registry_credential", cred.Id)
	if err := app.Save(stack); err != nil {
		t.Fatalf("save stack: %v", err)
	}

	authB64, insecureHosts := r.resolveRegistryAuth(stack)
	if authB64 == "" {
		t.Fatal("expected non-empty authB64 for stack with a registry_credential")
	}
	if len(insecureHosts) != 1 || insecureHosts[0] != "ghcr.io" {
		t.Fatalf("unexpected insecure hosts: %v", insecureHosts)
	}
}

func TestResolveRegistryAuthDegradesOnMissingCredential(t *testing.T) {
	app, stacks, _ := newResolveRegistryAuthTestApp(t)
	r := &Reconciler{app: app}

	stack := core.NewRecord(stacks)
	stack.Set("name", "dangling-credential")
	if err := app.Save(stack); err != nil {
		t.Fatalf("save stack: %v", err)
	}
	// Bypass relation-target validation (raw SQL) to simulate a credential
	// deleted out from under a stack that still references it — resolution
	// should degrade to "no auth" rather than fail the deploy.
	if _, err := app.DB().Update("stacks", dbx.Params{"registry_credential": "does-not-exist"}, dbx.HashExp{"id": stack.Id}).Execute(); err != nil {
		t.Fatalf("force dangling registry_credential: %v", err)
	}
	stack, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}

	authB64, insecureHosts := r.resolveRegistryAuth(stack)
	if authB64 != "" || insecureHosts != nil {
		t.Fatalf("expected resolution failure to degrade to empty result, got authB64=%q insecureHosts=%v", authB64, insecureHosts)
	}
}
