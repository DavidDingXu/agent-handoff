// Package images collects local image files referenced by a session and
// prepares them as bundle assets. Base64-embedded images stay inline in the
// session jsonl and need no asset handling.
package images

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

var (
	pathRe = regexp.MustCompile(`(?:path="([^"]+)"|(?:^|[\s"'])((?:/[A-Za-z0-9_.-]+){2,}/[^"' \t]+\.(?:png|jpe?g|gif|webp|bmp)))`)
	extRe  = regexp.MustCompile(`\.(png|jpe?g|gif|webp|bmp)$`)
)

// Collect finds local image files referenced by a session and returns the
// asset manifest plus the bytes of every found image. Relative paths are
// resolved against the source task's cwd, not the CLI's cwd.
func Collect(sessionBytes []byte, sourceCWD string) ([]bundle.ImageAsset, map[string][]byte, error) {
	bySource := map[string]*bundle.ImageAsset{}
	session.IterLines(sessionBytes, func(line session.Line) {
		if line.Obj == nil {
			return
		}
		for _, m := range pathRe.FindAllStringSubmatch(line.Raw, -1) {
			p := m[1]
			if p == "" {
				p = m[2]
			}
			if p == "" || !extRe.MatchString(strings.ToLower(p)) {
				continue
			}
			resolved := p
			if !filepath.IsAbs(p) && sourceCWD != "" {
				resolved = filepath.Join(sourceCWD, p)
			}
			if _, done := bySource[resolved]; done {
				continue
			}
			bySource[resolved] = &bundle.ImageAsset{
				SourcePath:  resolved,
				OriginalExt: strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), "."),
				Status:      "missing",
			}
		}
	})

	assets := []bundle.ImageAsset{}
	imageData := map[string][]byte{}
	for _, asset := range bySource {
		data, err := os.ReadFile(asset.SourcePath)
		if err != nil {
			assets = append(assets, *asset)
			continue
		}
		sum := sha256.Sum256(data)
		asset.SHA256 = hex.EncodeToString(sum[:])
		asset.Bytes = int64(len(data))
		asset.Mime = MimeForExt(asset.OriginalExt)
		asset.ID = "img_" + asset.SHA256[:16]
		asset.ZipPath = bundle.ImagesPrefix(bundle.AgentCodex) + asset.SHA256 + "." + asset.OriginalExt
		asset.Status = "copied"
		imageData[asset.ZipPath] = data
		assets = append(assets, *asset)
	}
	return assets, imageData, nil
}

// MimeForExt maps an image extension to a MIME type.
func MimeForExt(ext string) string {
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "bmp":
		return "image/bmp"
	}
	return "application/octet-stream"
}

// PathMap builds old-path -> new-path replacements for import.
func PathMap(assets []bundle.ImageAsset) map[string]string {
	m := map[string]string{}
	for _, a := range assets {
		if a.Copied() && a.TargetPath != "" {
			m[a.SourcePath] = a.TargetPath
		}
	}
	return m
}
