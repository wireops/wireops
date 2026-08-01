package integrations

// Category groups related integrations for display purposes. ID is the
// stable machine identifier, Label is the human-readable string that the
// API has always emitted under the "category" JSON field (see each
// provider's former Category() method) — Label values here must stay
// byte-identical to those old string literals, since
// internal/routes/integrations_golden_test.go pins them. Order controls the
// registry's All() sort order (ascending).
type Category struct {
	ID    string
	Label string
	Order int
}

var (
	CategoryReverseProxy   = Category{ID: "reverse-proxy", Label: "Reverse Proxy", Order: 0}
	CategoryLogging        = Category{ID: "logging", Label: "Logging", Order: 1}
	CategoryNotification   = Category{ID: "notification", Label: "Notification", Order: 2}
	CategorySecretBackend  = Category{ID: "secret-backend", Label: "Secret Backend", Order: 3}
	CategoryStorageBackend = Category{ID: "storage-backend", Label: "Storage Backend", Order: 4}
	CategorySourceControl  = Category{ID: "source-control", Label: "Source Control", Order: 5}
)
