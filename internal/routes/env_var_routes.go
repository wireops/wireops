package routes

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/secrets"
)

// envVarKeyPattern mirrors the frontend's envFileParser.ts KEY_PATTERN — the
// bulk route is also reachable directly (not just through the textarea
// parser), so it needs to enforce the same shape server-side.
var envVarKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envVarsBulkMaxBytes caps the bulk-upsert request body. A textarea paste or
// an imported .env is plain text, so anything past this is either a mistake
// or an attempt to make the server buffer/parse an oversized payload.
const envVarsBulkMaxBytes = 256 << 10

type bulkEnvVar struct {
	Key            string `json:"key"`
	Value          string `json:"value"`
	Secret         bool   `json:"secret"`
	SecretProvider string `json:"secret_provider"`
}

type bulkEnvVarsRequest struct {
	Mode string       `json:"mode"` // "replace" | "append"
	Vars []bulkEnvVar `json:"vars"`
}

type copyEnvVarsRequest struct {
	SourceStack string   `json:"source_stack"`
	Keys        []string `json:"keys"`
	Overwrite   bool     `json:"overwrite"`
}

// registerEnvVarRoutes exposes bulk-write operations for a stack's
// stack_env_vars that the auto-generated PocketBase CRUD can't do safely:
// atomic multi-row upsert (bulk edit / import) and a server-side copy from
// another stack (secrets never reach the browser to be copied back down).
func (rr routeRegistrar) registerEnvVarRoutes() {
	rr.r.POST("/api/custom/stacks/{id}/env-vars/bulk", func(e *core.RequestEvent) error {
		return rr.bulkUpsertStackEnvVars(e)
	}).Bind(apis.BodyLimit(envVarsBulkMaxBytes)).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.POST("/api/custom/stacks/{id}/env-vars/copy-from", func(e *core.RequestEvent) error {
		return rr.copyStackEnvVars(e)
	}).Bind(apis.BodyLimit(envVarsBulkMaxBytes)).BindFunc(rbac.Require(rbac.CapOperateStacks))
}

func (rr routeRegistrar) bulkUpsertStackEnvVars(e *core.RequestEvent) error {
	stackID := e.Request.PathValue("id")
	stack, err := rr.app.FindRecordById("stacks", stackID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
	}

	var body bulkEnvVarsRequest
	if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Mode != "replace" && body.Mode != "append" {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "mode must be \"replace\" or \"append\""})
	}

	seen := map[string]bool{}
	for _, v := range body.Vars {
		key := strings.TrimSpace(v.Key)
		if key == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "variable keys cannot be empty"})
		}
		if !envVarKeyPattern.MatchString(key) {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid key: " + key})
		}
		if seen[key] {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "duplicate key in payload: " + key})
		}
		seen[key] = true
	}

	existing, err := rr.app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	byKey := make(map[string]*core.Record, len(existing))
	for _, rec := range existing {
		byKey[rec.GetString("key")] = rec
	}

	col, err := rr.app.FindCollectionByNameOrId("stack_env_vars")
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	var created, updated, deleted int
	txErr := rr.app.RunInTransaction(func(txApp core.App) error {
		// RunInTransaction may retry the callback (e.g. on a busy-DB
		// conflict) — reset counters each attempt so a retried run doesn't
		// double-count against the response.
		created, updated, deleted = 0, 0, 0
		for _, v := range body.Vars {
			key := strings.TrimSpace(v.Key)
			rec, isExisting := byKey[key]
			if !isExisting {
				rec = core.NewRecord(col)
				rec.Set("stack", stack.Id)
				rec.Set("key", key)
				created++
			} else {
				updated++
			}

			// A blank value for an existing internal-provider secret means
			// "unchanged" (the client never receives the decrypted value to
			// round-trip) — leave the stored ciphertext alone. Mirrors the
			// single-row edit rule in EnvironmentVariablesCard.vue's
			// saveEditEnv, and the same guard the record hooks apply
			// independently in internal/hooks/pb_hooks.go. Only applies when
			// the row is staying secret — a downgrade to non-secret with a
			// blank value must clear the stored ciphertext, not leave it
			// sitting under secret=false where the UI would show it as
			// plaintext.
			skipValue := isExisting && v.Value == "" && v.Secret && rec.GetBool("secret") &&
				(rec.GetString("secret_provider") == "" || rec.GetString("secret_provider") == "internal")
			if !skipValue {
				rec.Set("value", v.Value)
			}
			rec.Set("secret", v.Secret)
			rec.Set("secret_provider", v.SecretProvider)
			if err := txApp.Save(rec); err != nil {
				return err
			}
		}

		if body.Mode == "replace" {
			for key, rec := range byKey {
				if seen[key] {
					continue
				}
				if err := txApp.Delete(rec); err != nil {
					return err
				}
				deleted++
			}
		}
		return nil
	})
	if txErr != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": txErr.Error()})
	}

	audit.RecordRequest(rr.app, e, audit.Event{
		Action:       "stack.env_vars.bulk_update",
		ResourceType: "stack",
		ResourceID:   stack.Id,
		Metadata:     map[string]any{"count": len(body.Vars), "mode": body.Mode},
	})

	return e.JSON(http.StatusOK, map[string]int{"created": created, "updated": updated, "deleted": deleted})
}

func (rr routeRegistrar) copyStackEnvVars(e *core.RequestEvent) error {
	targetID := e.Request.PathValue("id")
	target, err := rr.app.FindRecordById("stacks", targetID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
	}

	var body copyEnvVarsRequest
	if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	source, err := rr.app.FindRecordById("stacks", body.SourceStack)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "source stack not found"})
	}
	if len(body.Keys) == 0 {
		return e.JSON(http.StatusOK, map[string]any{"copied": 0, "skipped": []string{}})
	}

	if source.GetString("repository") != target.GetString("repository") {
		hasSops, err := stackHasSopsSecretsFile(rr.app, source)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if hasSops {
			return e.JSON(http.StatusBadRequest, map[string]string{
				"error": "cross-repository copy blocked: source stack has SOPS-managed secrets tied to its repository",
			})
		}
	}

	sourceRows, err := rr.app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": source.Id})
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	wanted := make(map[string]bool, len(body.Keys))
	for _, k := range body.Keys {
		wanted[k] = true
	}
	sourceByKey := make(map[string]*core.Record)
	for _, rec := range sourceRows {
		if wanted[rec.GetString("key")] {
			sourceByKey[rec.GetString("key")] = rec
		}
	}

	targetRows, err := rr.app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": target.Id})
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	targetByKey := make(map[string]*core.Record, len(targetRows))
	for _, rec := range targetRows {
		targetByKey[rec.GetString("key")] = rec
	}

	col, err := rr.app.FindCollectionByNameOrId("stack_env_vars")
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))

	var copied int
	var skipped []string
	txErr := rr.app.RunInTransaction(func(txApp core.App) error {
		copied = 0
		skipped = nil
		for _, key := range body.Keys {
			src, ok := sourceByKey[key]
			if !ok {
				skipped = append(skipped, key)
				continue
			}
			existingTarget, hasExisting := targetByKey[key]
			if hasExisting && !body.Overwrite {
				skipped = append(skipped, key)
				continue
			}

			value := src.GetString("value")
			secret := src.GetBool("secret")
			provider := src.GetString("secret_provider")
			if secret && (provider == "" || provider == "internal") {
				plaintext, err := crypto.Decrypt(value, secretKey)
				if err != nil {
					// A source row with an empty/corrupt ciphertext must not
					// abort the whole batch — skip just this key so the rest
					// of the requested copy still succeeds.
					skipped = append(skipped, key)
					continue
				}
				// Re-encrypt explicitly here rather than leaning solely on
				// the stack_env_vars OnRecordCreate/OnRecordUpdate hook
				// (internal/hooks/pb_hooks.go): the hook also does this on
				// Save, but doing it in the handler means this route stores
				// ciphertext even if hooks were ever mis-wired, and the
				// hook's own crypto.IsEncrypted check makes it a no-op on an
				// already-encrypted value, so there's no double-encryption.
				reencrypted, err := crypto.Encrypt(plaintext, secretKey)
				if err != nil {
					return err
				}
				value = reencrypted
			}

			var rec *core.Record
			if hasExisting {
				rec = existingTarget
			} else {
				rec = core.NewRecord(col)
				rec.Set("stack", target.Id)
				rec.Set("key", key)
			}
			rec.Set("value", value)
			rec.Set("secret", secret)
			rec.Set("secret_provider", provider)
			if err := txApp.Save(rec); err != nil {
				return err
			}
			copied++
		}
		return nil
	})
	if txErr != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": txErr.Error()})
	}

	if skipped == nil {
		skipped = []string{}
	}
	sort.Strings(skipped)

	audit.RecordRequest(rr.app, e, audit.Event{
		Action:       "stack.env_vars.copied_from",
		ResourceType: "stack",
		ResourceID:   target.Id,
		Metadata: map[string]any{
			"source_stack":     source.Id,
			"keys":             body.Keys,
			"count":            copied,
			"cross_repository": source.GetString("repository") != target.GetString("repository"),
		},
	})

	return e.JSON(http.StatusOK, map[string]any{"copied": copied, "skipped": skipped})
}

// stackHasSopsSecretsFile reports whether stack's repository checkout has a
// SOPS-encrypted secrets file next to its compose path. Mirrors the detection
// in registerSopsRoutes (GET .../sops-env-vars) but stops at "file exists" —
// it doesn't need to decrypt anything, just know whether cross-repo copy
// could silently drop SOPS-managed secrets.
func stackHasSopsSecretsFile(app core.App, stack *core.Record) (bool, error) {
	if stack.GetString("source_type") == "local" {
		return false, nil
	}
	repoID := stack.GetString("repository")
	if repoID == "" {
		return false, nil
	}
	if _, err := app.FindRecordById("repositories", repoID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	composePath := stack.GetString("compose_path")
	if err := safepath.ValidateComposePath(composePath); err != nil {
		return false, err
	}
	workDir := filepath.Join(config.GetReposWorkspace(), repoID)
	if composePath != "" && composePath != "." {
		workDir = filepath.Join(workDir, composePath)
	}

	path, err := secrets.FindSecretsFile(workDir)
	if err != nil {
		return false, err
	}
	return path != "", nil
}
