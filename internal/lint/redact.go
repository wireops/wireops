package lint

import (
	"path/filepath"
	"strings"

	"github.com/wireops/wireops/internal/config"
)

// RedactFindings strips server-side paths from a report's findings before
// they leave the process.
//
// Findings are not purely author-written text: `docker compose config`
// rewrites relative bind-mount sources into absolute server paths, so a
// volume-policy violation can carry "/data/repos/<id>/config" in its message.
// Any caller that returns findings to an API client or a validation error
// needs this — the compose lint preview route and the stacks OnRecordCreate
// hook both do.
//
// Message and Hint are the only free-text fields. Finding.Path is a dotted
// config location ("services.web.volumes"), not a filesystem path, and there
// is no Subject on a Finding — that field lives on policy.Violation and is
// folded into Message before it reaches here.
//
// Modifies in place.
func RedactFindings(findings []Finding) {
	for i := range findings {
		findings[i].Message = RedactWorkspacePaths(findings[i].Message)
		findings[i].Hint = RedactWorkspacePaths(findings[i].Hint)
	}
}

// RedactWorkspacePaths rewrites absolute paths under the repository workspace
// to a repo-relative form, so an error can be shown to a caller without
// describing where the server keeps its data.
func RedactWorkspacePaths(msg string) string {
	// Most specific first: the repo workspace usually lives under DATA_DIR, so
	// redacting DATA_DIR first would swallow the more informative "<repos>"
	// label. Each root is replaced in its trailing-separator form first so the
	// bare replacement cannot leave a doubled slash.
	//
	// "<workspace>/<repo-id>/svc/x.yml" becomes "<repos>/<repo-id>/svc/x.yml":
	// the repo id is the caller's own input, so only the server-side root is
	// worth hiding.
	for _, root := range []struct{ path, label string }{
		{config.GetReposWorkspace(), "<repos>"},
		{config.GetStacksStoragePath(), "<stacks>"},
		{config.GetDataDir(), "<data>"},
	} {
		if root.path == "" {
			continue
		}
		// These roots are configured relatively by default (DATA_DIR defaults
		// to "./data", and PB_DATA_DIR=./pb_data resolves DATA_DIR to "."),
		// while `docker compose config` rewrites bind-mount sources to
		// absolute paths — so matching has to happen in absolute form too, or
		// a root like "." never matches the real path it is meant to hide.
		// Resolving "." to an absolute path also closes the actual bug this
		// guards against: as a bare relative root, "." matched (and mangled)
		// every literal "." in a finding's message, e.g. an IP in a hint.
		abs, err := filepath.Abs(root.path)
		if err != nil {
			continue
		}
		trimmed := strings.TrimSuffix(abs, string(filepath.Separator))
		// A root that resolves to the filesystem root has nothing specific to
		// redact and would otherwise match the start of every absolute path.
		if trimmed == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, trimmed+string(filepath.Separator), root.label+"/")
		msg = strings.ReplaceAll(msg, trimmed, root.label)
	}
	return msg
}
