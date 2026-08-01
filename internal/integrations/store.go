package integrations

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
)

// maskSentinel is the placeholder the API emits in place of a Sensitive
// field's real value (see Mask), and the value Save recognizes on the way
// back in as "operator didn't change this field, carry the stored value
// through unchanged" rather than a genuinely new value to (re-)encrypt.
const maskSentinel = "••••••••"

// ErrMaskUnresolvable is wrapped into the error Save returns when cfg
// carries the mask sentinel for a field with no existing stored value to
// resolve it against — the one Save failure mode that is the caller's
// (client's) fault rather than a server-side condition (SECRET_KEY
// misconfigured, DB unreachable, etc.), so route handlers can map it to 400
// while everything else maps to 500.
var ErrMaskUnresolvable = errors.New("integrations: masked field unresolvable")

// MaskSecret returns secret's masked representation for API responses: the
// sentinel if secret is non-empty, "" if it's already empty (nothing to
// mask). internal/notify.MaskSecret delegates here — moved from that
// package since masking is now a Store concern shared by every integration,
// not just notify's four providers.
func MaskSecret(secret string) string {
	if strings.TrimSpace(secret) == "" {
		return ""
	}
	return maskSentinel
}

// Instance is one integration's persisted state, as resolved against its
// registered Descriptor: Config is always fully decrypted (every
// Descriptor.EncryptedKeys() field), ready to hand to provider logic or to
// Mask before it reaches an API response.
type Instance struct {
	Slug    string
	Enabled bool
	Config  map[string]any
	Locked  bool
}

// Store is the generic load/save/mask surface for the "integrations"
// collection, replacing the hand-rolled fetch+decrypt+encrypt+mask logic
// that used to be duplicated across internal/routes/routes_register.go,
// internal/notify, internal/secrets, and internal/backup.
type Store struct {
	app       core.App
	secretKey []byte
}

// NewStore builds a Store backed by app, encrypting/decrypting
// Descriptor.EncryptedKeys() fields with secretKey (the app's normalized
// SECRET_KEY — see crypto.NormalizeSecretKey).
func NewStore(app core.App, secretKey []byte) Store {
	return Store{app: app, secretKey: secretKey}
}

// descriptorFor returns slug's registered Descriptor, or the zero
// Descriptor if it isn't registered — every Descriptor method used here
// (SensitiveKeys/EncryptedKeys/RequiredKeys) is nil-safe on a zero value.
func descriptorFor(slug string) Descriptor {
	entry, _ := Get(slug)
	return entry.Descriptor
}

// findRecord returns slug's "integrations" row, or nil (with a nil error)
// if none exists yet — including when the "integrations" collection itself
// doesn't exist (some test fixtures never create it). Uses
// FindFirstRecordByFilter rather than FindAllRecords specifically because
// PocketBase surfaces a missing collection as sql.ErrNoRows through the
// former but as an opaque query-build error through the latter.
func (s Store) findRecord(slug string) (*core.Record, error) {
	rec, err := s.app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": slug})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return rec, nil
}

// Load returns slug's current Instance, with every Descriptor.EncryptedKeys()
// config field decrypted. A slug with no stored row yet returns
// Instance{Slug: slug, Enabled: false, Config: map[string]any{}, Locked:
// descriptor.Locked} — mirroring the pre-Store "no row = not enabled"
// semantics, except Locked still reflects a descriptor-level lock (e.g.
// sops/github, which are always locked regardless of whether a row exists).
func (s Store) Load(slug string) (Instance, error) {
	d := descriptorFor(slug)

	rec, err := s.findRecord(slug)
	if err != nil {
		return Instance{}, fmt.Errorf("integrations: failed to query %q: %w", slug, err)
	}
	if rec == nil {
		return Instance{Slug: slug, Enabled: false, Config: map[string]any{}, Locked: d.Locked}, nil
	}

	return s.decodeRecord(slug, d, rec)
}

// decodeRecord turns rec (an "integrations" row already known to belong to
// slug) into its decrypted Instance, per d's EncryptedKeys.
func (s Store) decodeRecord(slug string, d Descriptor, rec *core.Record) (Instance, error) {
	var cfg map[string]any
	if err := rec.UnmarshalJSONField("config", &cfg); err != nil {
		return Instance{}, fmt.Errorf("integrations: failed to unmarshal %q config: %w", slug, err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}

	for _, key := range d.EncryptedKeys() {
		raw, ok := cfg[key]
		if !ok {
			continue
		}
		switch val := raw.(type) {
		case string:
			if val == "" {
				continue
			}
			plaintext, err := crypto.Decrypt(val, s.secretKey)
			if err != nil {
				return Instance{}, fmt.Errorf("integrations: failed to decrypt %s.%s: %w", slug, key, err)
			}
			cfg[key] = string(plaintext)
		case []interface{}:
			for i, item := range val {
				kv, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				ciphertext, ok := kv["value"].(string)
				if !ok || ciphertext == "" {
					continue
				}
				plaintext, err := crypto.Decrypt(ciphertext, s.secretKey)
				if err != nil {
					return Instance{}, fmt.Errorf("integrations: failed to decrypt %s.%s[%d]: %w", slug, key, i, err)
				}
				kv["value"] = string(plaintext)
				val[i] = kv
			}
			cfg[key] = val
		}
	}

	return Instance{
		Slug:    slug,
		Enabled: rec.GetBool("enabled"),
		Config:  cfg,
		Locked:  d.Locked || rec.GetBool("locked"),
	}, nil
}

// LoadAll returns every registered integration's Instance in one query
// (FindAllRecords, not one FindFirstRecordByFilter per slug), replacing the
// N+1 pattern a per-slug Load loop would otherwise produce for endpoints
// like GET /api/custom/integrations that need every integration's state at
// once.
//
// A slug whose stored config fails to decrypt (e.g. a pre-existing plaintext
// row for a field a descriptor has since marked Encrypted) is logged and
// downgraded to the same "not configured" zero Instance a missing row would
// produce, rather than failing the whole batch — one broken integration
// must not take down the list for every other one.
//
// A failure of the underlying FindAllRecords query itself (as opposed to a
// single row's decryption) is logged and returned as err, alongside an empty
// map, so callers can choose to surface a 500 rather than silently reporting
// "no integrations configured."
func (s Store) LoadAll() (map[string]Instance, error) {
	out := make(map[string]Instance)

	recs, err := s.app.FindAllRecords("integrations")
	if err != nil {
		log.Printf("[integrations] failed to query integrations collection: %v", err)
		return out, fmt.Errorf("integrations: failed to query integrations collection: %w", err)
	}

	byRecordSlug := make(map[string]*core.Record, len(recs))
	for _, rec := range recs {
		byRecordSlug[rec.GetString("slug")] = rec
	}

	for _, entry := range All() {
		slug := entry.Descriptor.Slug
		d := entry.Descriptor

		rec, ok := byRecordSlug[slug]
		if !ok {
			out[slug] = Instance{Slug: slug, Enabled: false, Config: map[string]any{}, Locked: d.Locked}
			continue
		}

		instance, err := s.decodeRecord(slug, d, rec)
		if err != nil {
			log.Printf("[integrations] failed to load %q, treating as unconfigured: %v", slug, err)
			out[slug] = Instance{Slug: slug, Enabled: false, Config: map[string]any{}, Locked: d.Locked}
			continue
		}
		out[slug] = instance
	}

	return out, nil
}

// Save resolves cfg's mask sentinel against the currently-stored (still
// encrypted, for Encrypted fields) value for every Sensitive key — a field
// left as the sentinel means "operator didn't change this," so its existing
// stored value (ciphertext or plaintext, whatever it already was) is carried
// through verbatim rather than treated as a new value to encrypt. Every
// other Descriptor.EncryptedKeys() field present in cfg is treated as
// genuinely new and encrypted before the row is upserted by slug.
//
// This fully replaces the old caller-supplied alreadyEncryptedKeys
// bookkeeping (routes_register.go's resolveMaskedIntegrationConfig +
// encryptIntegrationConfig): the sentinel-resolution and the
// encrypt-skip-if-carried-over decision both happen here, together, so
// there's no way for a caller to get them out of sync and double-encrypt.
func (s Store) Save(slug string, enabled bool, cfg map[string]any) error {
	d := descriptorFor(slug)

	// Shallow-copy so mask-resolution and encryption below never mutate the
	// caller's map — same contract Mask already gives its callers.
	src := cfg
	cfg = make(map[string]any, len(src))
	for k, v := range src {
		cfg[k] = v
	}

	rec, err := s.findRecord(slug)
	if err != nil {
		return fmt.Errorf("integrations: failed to query %q: %w", slug, err)
	}

	var existingCfg map[string]any
	if rec != nil {
		if err := rec.UnmarshalJSONField("config", &existingCfg); err != nil {
			return fmt.Errorf("integrations: failed to unmarshal existing %q config: %w", slug, err)
		}
	}

	carriedOver := make(map[string]bool)
	carriedOverKV := make(map[string]map[int]bool)
	for _, key := range d.SensitiveKeys() {
		raw, ok := cfg[key]
		if !ok {
			continue
		}
		switch val := raw.(type) {
		case string:
			if val != maskSentinel {
				continue
			}
			existingVal, ok := existingCfg[key].(string)
			if !ok || existingVal == "" {
				return fmt.Errorf("%w: cannot resolve masked %s: no saved value found in existing %s configuration", ErrMaskUnresolvable, key, slug)
			}
			cfg[key] = existingVal
			carriedOver[key] = true
		case []interface{}:
			existingByHeaderKey := make(map[string]string)
			if existingList, ok := existingCfg[key].([]interface{}); ok {
				for _, item := range existingList {
					if m, ok := item.(map[string]interface{}); ok {
						if hk, ok := m["key"].(string); ok && hk != "" {
							if hv, ok := m["value"].(string); ok {
								existingByHeaderKey[hk] = hv
							}
						}
					}
				}
			}
			resolved := make([]interface{}, len(val))
			for i, item := range val {
				kv, ok := item.(map[string]interface{})
				if !ok {
					resolved[i] = item
					continue
				}
				kvCopy := make(map[string]interface{}, len(kv))
				for k, v := range kv {
					kvCopy[k] = v
				}
				if hv, ok := kvCopy["value"].(string); ok && hv == maskSentinel {
					hk, _ := kvCopy["key"].(string)
					existingVal, ok := existingByHeaderKey[hk]
					if !ok || existingVal == "" {
						return fmt.Errorf("%w: cannot resolve masked %s: no saved value found in existing %s configuration", ErrMaskUnresolvable, key, slug)
					}
					kvCopy["value"] = existingVal
					if carriedOverKV[key] == nil {
						carriedOverKV[key] = make(map[int]bool)
					}
					carriedOverKV[key][i] = true
				}
				resolved[i] = kvCopy
			}
			cfg[key] = resolved
		}
	}

	for _, key := range d.EncryptedKeys() {
		if carriedOver[key] {
			continue
		}
		raw, ok := cfg[key]
		if !ok {
			continue
		}
		switch val := raw.(type) {
		case string:
			if val == "" {
				continue
			}
			if len(s.secretKey) != 32 {
				return fmt.Errorf("integrations: SECRET_KEY must be exactly 32 bytes to encrypt %s.%s (got %d)", slug, key, len(s.secretKey))
			}
			encrypted, err := crypto.Encrypt([]byte(val), s.secretKey)
			if err != nil {
				return fmt.Errorf("integrations: failed to encrypt %s.%s: %w", slug, key, err)
			}
			cfg[key] = encrypted
		case []interface{}:
			skip := carriedOverKV[key]
			for i, item := range val {
				if skip[i] {
					continue
				}
				kv, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				hv, ok := kv["value"].(string)
				if !ok || hv == "" {
					continue
				}
				if len(s.secretKey) != 32 {
					return fmt.Errorf("integrations: SECRET_KEY must be exactly 32 bytes to encrypt %s.%s[%d] (got %d)", slug, key, i, len(s.secretKey))
				}
				encrypted, err := crypto.Encrypt([]byte(hv), s.secretKey)
				if err != nil {
					return fmt.Errorf("integrations: failed to encrypt %s.%s[%d]: %w", slug, key, i, err)
				}
				kv["value"] = encrypted
				val[i] = kv
			}
			cfg[key] = val
		}
	}

	if rec == nil {
		col, err := s.app.FindCollectionByNameOrId("integrations")
		if err != nil {
			return fmt.Errorf("integrations: failed to find integrations collection: %w", err)
		}
		rec = core.NewRecord(col)
		rec.Set("slug", slug)
	}
	rec.Set("enabled", enabled)
	rec.Set("config", cfg)
	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("integrations: failed to save %q: %w", slug, err)
	}
	return nil
}

// Mask returns a copy of cfg with every Descriptor.SensitiveKeys() field
// replaced by the mask sentinel, for handing decrypted config to an API
// response. Never mutates cfg.
func (s Store) Mask(slug string, cfg map[string]any) map[string]any {
	d := descriptorFor(slug)
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	for _, key := range d.SensitiveKeys() {
		switch val := out[key].(type) {
		case string:
			if val != "" {
				out[key] = MaskSecret(val)
			}
		case []interface{}:
			masked := make([]interface{}, len(val))
			for i, item := range val {
				kv, ok := item.(map[string]interface{})
				if !ok {
					masked[i] = item
					continue
				}
				kvCopy := make(map[string]interface{}, len(kv))
				for k, v := range kv {
					kvCopy[k] = v
				}
				if hv, ok := kvCopy["value"].(string); ok && hv != "" {
					kvCopy["value"] = MaskSecret(hv)
				}
				masked[i] = kvCopy
			}
			out[key] = masked
		}
	}
	return out
}

// Bound pairs one enabled integration's decrypted Config with its
// registered Impl, typed as T — the result of filtering All() down to
// entries that are both enabled and satisfy a given capability interface.
// Not yet consumed anywhere (wiring real Notifier/SecretResolver/
// StorageBackend capability implementations to it is phase 3), but the
// shape is settled now so that wiring is additive rather than another
// registry/Store signature change.
type Bound[T any] struct {
	Slug   string
	Config map[string]any
	Impl   T
}

// EnabledWith returns Bound[T] for every registered integration that is
// both enabled (per Store) and whose Impl satisfies T, in All()'s
// deterministic order.
func EnabledWith[T any](s Store) []Bound[T] {
	instances, err := s.LoadAll()
	if err != nil {
		return nil
	}

	var out []Bound[T]
	for _, e := range All() {
		impl, ok := e.Impl.(T)
		if !ok {
			continue
		}
		instance, ok := instances[e.Descriptor.Slug]
		if !ok || !instance.Enabled {
			continue
		}
		out = append(out, Bound[T]{Slug: e.Descriptor.Slug, Config: instance.Config, Impl: impl})
	}
	return out
}
