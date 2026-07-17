// Package testutil holds fixture builders shared across test files in
// multiple packages (secrets, sync, routes, ...).
package testutil

import (
	"testing"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/cmd/sops/formats"
	sopsconfig "github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/version"
)

// EncryptForAge builds a SOPS-encrypted YAML document for the given age
// recipient, bypassing sops-wrapper's Encrypt (which only wires up
// aws/gcp/azure/vault key groups, not age) by driving the same
// getsops/sops/v3 primitives directly with an age.MasterKey.
func EncryptForAge(t *testing.T, publicKey string, plaintext []byte) []byte {
	t.Helper()

	store := common.StoreForFormat(formats.Yaml, sopsconfig.NewStoresConfig())
	branches, err := store.LoadPlainFile(plaintext)
	if err != nil {
		t.Fatalf("LoadPlainFile: %v", err)
	}

	masterKey, err := sopsage.MasterKeyFromRecipient(publicKey)
	if err != nil {
		t.Fatalf("MasterKeyFromRecipient: %v", err)
	}

	tree := sops.Tree{
		Branches: branches,
		Metadata: sops.Metadata{
			KeyGroups: []sops.KeyGroup{{masterKey}},
			Version:   version.Version,
		},
	}

	dataKey, errs := tree.GenerateDataKeyWithKeyServices([]keyservice.KeyServiceClient{keyservice.NewLocalClient()})
	if len(errs) > 0 {
		t.Fatalf("GenerateDataKeyWithKeyServices: %v", errs)
	}

	if err := common.EncryptTree(common.EncryptTreeOpts{DataKey: dataKey, Tree: &tree, Cipher: aes.NewCipher()}); err != nil {
		t.Fatalf("EncryptTree: %v", err)
	}

	encBytes, err := store.EmitEncryptedFile(tree)
	if err != nil {
		t.Fatalf("EmitEncryptedFile: %v", err)
	}
	return encBytes
}
