package routes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/manifest"
)

func wireopsValidationErrors(err error) []string {
	var valErr *manifest.ValidationError
	if errors.As(err, &valErr) {
		return valErr.Errors
	}
	return []string{err.Error()}
}

// resolveWireopsComposeFile locates the single compose file alongside a
// wireops.yaml at wireopsFile (relative to repoDir), non-recursively. It
// never returns a Go error for "not found"/"ambiguous" cases — those are
// reported back via def.ResolutionError so the frontend can surface them
// without treating a valid wireops.yaml as a 422.
func resolveWireopsComposeFile(repoDir, wireopsFile string, def *manifest.Definition) {
	cleanWireopsFile, err := safepath.CleanRelativePath(wireopsFile)
	if err != nil {
		def.ResolutionError = fmt.Sprintf("invalid wireops file path %q: %v", wireopsFile, err)
		return
	}

	dir := filepath.Dir(cleanWireopsFile)
	absDir := filepath.Join(repoDir, dir)

	entries, err := os.ReadDir(absDir)
	if err != nil {
		def.ResolutionError = fmt.Sprintf("cannot read directory %q: %v", dir, err)
		return
	}

	var matches []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(absDir, entry.Name()))
		if err != nil {
			continue
		}
		if compose.IsComposeFile(data) {
			matches = append(matches, entry.Name())
		}
	}
	sort.Strings(matches)

	switch len(matches) {
	case 0:
		def.ResolutionError = fmt.Sprintf("no compose file found in %q", dir)
	case 1:
		def.ResolvedComposePath = dir
		def.ResolvedComposeFile = matches[0]
	default:
		def.ResolutionError = fmt.Sprintf("multiple compose files in %q, ambiguous: [%s]", dir, strings.Join(matches, ", "))
	}
}
