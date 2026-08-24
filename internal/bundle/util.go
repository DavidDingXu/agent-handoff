package bundle

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors surfaced to the CLI layer.
var (
	ErrNotShareFile        = errors.New("not a agent-handoff file")
	ErrMissingManifest     = errors.New("invalid share file: missing manifest.json")
	ErrMissingSession      = errors.New("invalid share file: missing session entry")
	ErrBadArtifactType     = errors.New("invalid share file: unsupported artifact type")
	ErrUnsupportedVersion  = errors.New("invalid share file: unsupported format version")
	ErrUnsupportedAgent    = errors.New("invalid share file: unsupported source agent")
	ErrChecksumAlgorithm   = errors.New("invalid checksums: unsupported algorithm")
	ErrChecksumEntryNeeded = errors.New("invalid share file: missing checksums.json")
	ErrArchiveLimit        = errors.New("invalid share file: archive exceeds safety limits")
)

// ImageAsset describes one image bundled for the receiver.
type ImageAsset struct {
	ID          string `json:"id"`
	SourcePath  string `json:"source_path"`
	ZipPath     string `json:"zip_path"`
	SHA256      string `json:"sha256"`
	Mime        string `json:"mime"`
	Bytes       int64  `json:"bytes"`
	OriginalExt string `json:"original_ext"`
	Status      string `json:"status"` // copied | missing
	TargetPath  string `json:"target_path,omitempty"`
}

// Copied reports whether the image bytes made it into the zip.
func (a ImageAsset) Copied() bool { return a.Status == "copied" }

// CountCopied returns the number of bundled images.
func CountCopied(images []ImageAsset) int {
	n := 0
	for _, img := range images {
		if img.Copied() {
			n++
		}
	}
	return n
}

// CountMissing returns the number of referenced-but-absent images.
func CountMissing(images []ImageAsset) int {
	n := 0
	for _, img := range images {
		if img.Status == "missing" {
			n++
		}
	}
	return n
}

// imagesManifest is the images.json payload.
type imagesManifest struct {
	Schema string       `json:"schema"`
	Count  int          `json:"count"`
	Images []ImageAsset `json:"images"`
}

// Slugify converts a title into a filesystem-friendly slug. It falls back
// to "codex-task" when nothing usable remains (e.g. pure CJK titles).
func Slugify(title string) string {
	const maxLen = 60
	var sb strings.Builder
	lastDash := true // suppress leading dashes
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastDash = false
		case r == ' ', r == '-', r == '_', r == '.', r == '/', r == ':':
			if !lastDash {
				sb.WriteByte('-')
				lastDash = true
			}
		}
		if sb.Len() >= maxLen {
			break
		}
	}
	slug := strings.Trim(sb.String(), "-")
	if slug == "" {
		return "codex-task"
	}
	if len(slug) > maxLen {
		slug = slug[:maxLen]
	}
	return slug
}

// DefaultSharePath builds ./<slug>.agent-handoff.zip.
func DefaultSharePath(title string) string {
	return fmt.Sprintf("./%s.agent-handoff.zip", Slugify(title))
}
