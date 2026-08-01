package integrations

// FieldKind describes the shape of a ConfigField's value, driving how the
// (not-yet-built) frontend schema-driven config form renders and validates
// it. Kinds are purely descriptive in phase 2 — nothing here changes
// validation/encryption behavior, that still lives in routes_register.go's
// key-list-driven helpers.
type FieldKind string

const (
	FieldText     FieldKind = "text"
	FieldPassword FieldKind = "password"
	FieldURL      FieldKind = "url"
	FieldBool     FieldKind = "bool"
	FieldSelect   FieldKind = "select"
	FieldList     FieldKind = "list"
	FieldKV       FieldKind = "kv"
)

// Option is one selectable value for a FieldSelect field.
type Option struct {
	Value string
	Label string
}

// FieldRules carries optional, kind-specific validation hints for a
// ConfigField (e.g. restricting a FieldURL to a set of allowed hosts).
type FieldRules struct {
	AllowedHosts []string
	PathPrefix   string
	Scheme       string
	Pattern      string
}

// ConfigField declaratively describes one key in an integration's config
// map, replacing the hand-maintained
// sensitive/encrypted/required-key switches that used to live in
// internal/routes/routes_register.go.
type ConfigField struct {
	Key       string
	Label     string
	Help      string
	Kind      FieldKind
	Required  bool
	Sensitive bool
	Encrypted bool
	Default   any
	Options   []Option
	Rules     FieldRules
}

// Descriptor is the static, declarative metadata for one registered
// integration — replacing the old Integration interface's
// Slug()/Name()/Category() methods with data, so the registry (and anything
// downstream, e.g. API responses, config validation) can be driven from a
// single source of truth per integration.
type Descriptor struct {
	Slug         string
	Name         string
	Category     Category
	Description  string
	DocURL       string
	Icon         string
	Locked       bool
	Fields       []ConfigField
	Capabilities []CapabilityID
}

// SensitiveKeys returns the Key of every ConfigField marked Sensitive, in
// declaration order.
func (d Descriptor) SensitiveKeys() []string {
	return d.filterKeys(func(f ConfigField) bool { return f.Sensitive })
}

// EncryptedKeys returns the Key of every ConfigField marked Encrypted, in
// declaration order.
func (d Descriptor) EncryptedKeys() []string {
	return d.filterKeys(func(f ConfigField) bool { return f.Encrypted })
}

// RequiredKeys returns the Key of every ConfigField marked Required, in
// declaration order.
func (d Descriptor) RequiredKeys() []string {
	return d.filterKeys(func(f ConfigField) bool { return f.Required })
}

func (d Descriptor) filterKeys(match func(ConfigField) bool) []string {
	var keys []string
	for _, f := range d.Fields {
		if match(f) {
			keys = append(keys, f.Key)
		}
	}
	return keys
}

// HasCapability reports whether d declares id among its Capabilities.
func (d Descriptor) HasCapability(id CapabilityID) bool {
	for _, c := range d.Capabilities {
		if c == id {
			return true
		}
	}
	return false
}

// Field returns the ConfigField with the given Key, if present.
func (d Descriptor) Field(key string) (ConfigField, bool) {
	for _, f := range d.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return ConfigField{}, false
}
