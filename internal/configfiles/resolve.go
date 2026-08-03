// Package configfiles resolves git-committed "config" files/directories,
// declared via compose service annotations (stacks) or job.yaml `configs:`
// entries (jobs), into plaintext content server-side, and tracks what was
// resolved in PocketBase for UI/drift visibility. It is the single seam a
// future encrypted-config source (e.g. SOPS, mirroring internal/sync's
// loadSopsEnv/overlaySopsEnv for env vars) would hook into.
package configfiles

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wireops/wireops/internal/safepath"
)

const (
	// MaxFileBytes caps the size of a single resolved config file.
	MaxFileBytes = 1 << 20 // 1MB
	// MaxTotalBytes caps the combined size of all config files resolved for
	// a single stack/job dispatch.
	MaxTotalBytes = 5 << 20 // 5MB
)

// Source identifies a config entry to resolve: a name and its repo-relative
// source path, which may point at a single file or a directory. A directory
// source expands into one ResolvedFile per file found recursively under it.
type Source struct {
	Name string
	Path string
}

// ResolvedFile is the plaintext content of a single resolved file, plus its
// checksum for change detection and tracking. RelPath is empty when Source
// pointed directly at a file; when Source pointed at a directory, RelPath is
// this file's slash-separated path relative to that directory, and the
// caller is expected to mount/write it at <declared target>/<RelPath>.
type ResolvedFile struct {
	Name       string
	SourcePath string
	RelPath    string
	Content    []byte
	Checksum   string // sha256 hex
}

// Resolve reads each source's file(s) from workDir (a repository checkout),
// enforcing per-file and total size caps (shared across the whole call) and
// repo containment. It returns an error naming the offending entry on any
// failure, so callers can surface it the same way they surface a missing or
// invalid compose file.
func Resolve(workDir string, sources []Source) ([]ResolvedFile, error) {
	var results []ResolvedFile
	var total int64

	for _, s := range sources {
		clean, err := safepath.CleanRelativePath(s.Path)
		if err != nil {
			return nil, fmt.Errorf("config %q: invalid path %q: %w", s.Name, s.Path, err)
		}

		full, err := containedJoin(workDir, clean)
		if err != nil {
			return nil, fmt.Errorf("config %q: %w", s.Name, err)
		}

		// Lstat, not Stat: a symlink inside the repo can point anywhere on
		// the host filesystem while its own lexical path stays within
		// workDir, so following it would bypass containedJoin's check
		// entirely. Reject symlinks (and anything else non-regular) here,
		// before ever resolving/reading the target.
		info, err := os.Lstat(full)
		if err != nil {
			return nil, fmt.Errorf("config %q: cannot stat %q: %w", s.Name, s.Path, err)
		}

		if info.IsDir() {
			files, newTotal, err := resolveDir(s, full, total)
			if err != nil {
				return nil, err
			}
			total = newTotal
			results = append(results, files...)
			continue
		}

		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("config %q: %q is not a regular file (symlinks are not allowed)", s.Name, s.Path)
		}

		if err := checkSize(s.Name, s.Path, info.Size(), &total); err != nil {
			return nil, err
		}
		content, err := os.ReadFile(full) // #nosec G304 -- full is validated above via safepath.CleanRelativePath + containedJoin
		if err != nil {
			return nil, fmt.Errorf("config %q: cannot read %q: %w", s.Name, s.Path, err)
		}
		results = append(results, ResolvedFile{
			Name:       s.Name,
			SourcePath: s.Path,
			Content:    content,
			Checksum:   checksum(content),
		})
	}

	return results, nil
}

// resolveDir walks full recursively, returning one ResolvedFile per regular
// file found, with RelPath set to its slash-separated path under full.
func resolveDir(s Source, full string, total int64) ([]ResolvedFile, int64, error) {
	var files []ResolvedFile
	err := filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(full, p)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)

		// d.Info() reflects the directory entry itself (lstat semantics),
		// not any symlink target — same reasoning as the top-level Lstat
		// check above: a symlink here could point outside workDir entirely.
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("cannot stat %q: %w", relSlash, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%q is not a regular file (symlinks are not allowed)", relSlash)
		}
		if err := checkSize(s.Name, s.Path+"/"+relSlash, info.Size(), &total); err != nil {
			return err
		}

		content, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("cannot read %q: %w", relSlash, err)
		}
		files = append(files, ResolvedFile{
			Name:       s.Name,
			SourcePath: s.Path + "/" + relSlash,
			RelPath:    relSlash,
			Content:    content,
			Checksum:   checksum(content),
		})
		return nil
	})
	if err != nil {
		return nil, total, fmt.Errorf("config %q: %w", s.Name, err)
	}
	return files, total, nil
}

func checkSize(name, path string, size int64, total *int64) error {
	if size > MaxFileBytes {
		return fmt.Errorf("config %q: file %q is %d bytes, exceeds %d byte limit", name, path, size, MaxFileBytes)
	}
	*total += size
	if *total > MaxTotalBytes {
		return fmt.Errorf("config %q: total resolved config size exceeds %d byte limit", name, MaxTotalBytes)
	}
	return nil
}

// containedJoin joins rel onto workDir and verifies the resolved absolute
// path stays inside workDir, mirroring the containment check in
// manifest.ParseWireopsFile and job.ParseJobFile.
func containedJoin(workDir, rel string) (string, error) {
	baseAbs, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve work dir: %w", err)
	}
	fullAbs, err := filepath.Abs(filepath.Join(baseAbs, rel))
	if err != nil {
		return "", fmt.Errorf("cannot resolve path: %w", err)
	}
	relCheck, err := filepath.Rel(baseAbs, fullAbs)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes repository directory: %q", rel)
	}
	return fullAbs, nil
}

func checksum(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}
