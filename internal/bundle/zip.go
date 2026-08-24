package bundle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/safety"
)

const (
	maxZipEntries           = 1024
	maxZipEntryBytes uint64 = 128 << 20
	maxZipTotalBytes uint64 = 256 << 20
)

// WriterInput is everything needed to emit a share zip.
type WriterInput struct {
	Manifest  *Manifest
	Session   []byte            // raw native session
	Meta      map[string]any    // sender-side metadata (thread row / index entry)
	Images    []ImageAsset      // image manifest (may be nil)
	ImageData map[string][]byte // zip path -> bytes, for copied images
	Neutral   neutral.Transcript
}

// WriteZip packs the bundle into path (v2 layout).
func WriteZip(path string, in WriterInput) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := writeZip(f, in); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// WriteZipBytes packs a bundle in memory. Link shares use this path so the
// plaintext archive never has to touch disk when upload succeeds.
func WriteZipBytes(in WriterInput) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeZip(&buf, in); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeZip(dst io.Writer, in WriterInput) error {
	zw := zip.NewWriter(dst)
	write := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}

	agent := in.Manifest.SourceAgent
	files := map[string][]byte{}
	files[ManifestEntry] = nil // placeholder, filled below

	metaBytes := marshalIndent(in.Meta)
	files[MetaEntry(agent)] = metaBytes
	files[SessionEntry(agent)] = in.Session

	if in.Neutral.Schema == "" {
		in.Neutral = neutral.Transcript{Schema: neutral.Schema, SourceAgent: agent}
	}
	files[NeutralEntry] = marshalIndent(in.Neutral)
	files[RestoreEntry] = []byte(neutral.RestoreMarkdown(in.Neutral, agent))
	files[SafetyEntry] = marshalIndent(map[string]any{
		"algorithm": "regex-high-confidence",
		"rules":     safety.Rules(),
	})

	if in.Images != nil {
		files[ImagesEntry(agent)] = marshalIndent(imagesManifest{
			Schema: "agent-handoff.images.v1",
			Count:  len(in.Images),
			Images: in.Images,
		})
		for zipPath, data := range in.ImageData {
			files[zipPath] = data
		}
	}

	// Manifest last fields: file list. checksums.json must not list itself:
	// a self-referential hash cannot be verified.
	var names []string
	for name := range files {
		if name != ManifestEntry {
			names = append(names, name)
		}
	}
	names = append(names, ReadmeEntry)
	sortStrings(names)
	in.Manifest.Files = names
	files[ManifestEntry] = marshalIndent(in.Manifest)

	// Every entry except checksums.json itself must be covered.
	files[ReadmeEntry] = []byte(agentReadme(in.Manifest))
	checksums := BuildChecksums(files)
	files[ChecksumsEntry] = checksums.Marshal()

	// Deterministic write order.
	all := make([]string, 0, len(files))
	for name := range files {
		all = append(all, name)
	}
	sortStrings(all)
	for _, name := range all {
		if err := write(name, files[name]); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return zw.Close()
}

// ReadResult is a loaded bundle.
type ReadResult struct {
	Manifest  *Manifest
	Session   []byte
	Meta      map[string]any
	Images    []ImageAsset
	ImageData map[string][]byte // zip path -> bytes
	Neutral   neutral.Transcript
	Files     map[string][]byte // every entry except image payloads
}

// ReadZip opens and validates a share zip (v2 or v1 codex layout).
func ReadZip(path string) (*ReadResult, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotShareFile, err)
	}
	defer zr.Close()

	files, err := readZipFiles(zr.File)
	if err != nil {
		return nil, err
	}
	return decode(files)
}

// ReadZipBytes opens and validates a share zip from memory (used by the
// link importer after decryption).
func ReadZipBytes(data []byte) (*ReadResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotShareFile, err)
	}
	files, err := readZipFiles(zr.File)
	if err != nil {
		return nil, err
	}
	return decode(files)
}

func decode(files map[string][]byte) (*ReadResult, error) {
	manifestData, ok := files[ManifestEntry]
	if !ok || manifestData == nil {
		return nil, ErrMissingManifest
	}
	var m Manifest
	if err := unmarshalStrict(manifestData, &m); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	if m.ArtifactType != ArtifactType {
		return nil, fmt.Errorf("%w %q", ErrBadArtifactType, m.ArtifactType)
	}
	if m.FormatVersion != Version1 && m.FormatVersion != Version2 {
		return nil, fmt.Errorf("%w %d", ErrUnsupportedVersion, m.FormatVersion)
	}
	// v1 compatibility: flat codex layout, no source_agent field.
	agent := m.SourceAgent
	if m.FormatVersion == Version1 {
		agent = AgentCodex
	}
	if !IsSupportedAgent(agent) {
		return nil, fmt.Errorf("%w %q", ErrUnsupportedAgent, m.SourceAgent)
	}

	sessionData, ok := files[SessionEntry(agent)]
	if !ok && agent == AgentCodex {
		// v1 flat layout.
		sessionData, ok = files["session.jsonl"]
	}
	if !ok {
		return nil, ErrMissingSession
	}

	res := &ReadResult{
		Manifest:  &m,
		Session:   sessionData,
		ImageData: map[string][]byte{},
		Files:     map[string][]byte{},
	}

	if meta := files[MetaEntry(agent)]; meta != nil {
		res.Meta = unmarshalMap(meta)
	} else if row := files["thread-row.json"]; row != nil { // v1
		res.Meta = unmarshalMap(row)
	}

	if imgs := files[ImagesEntry(agent)]; imgs != nil {
		var im imagesManifest
		if err := unmarshalStrict(imgs, &im); err == nil {
			res.Images = im.Images
		}
	} else if imgs := files["images.json"]; imgs != nil { // v1
		var im imagesManifest
		if err := unmarshalStrict(imgs, &im); err == nil {
			res.Images = im.Images
		}
	}
	prefix := ImagesPrefix(agent)
	for name, data := range files {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			res.ImageData[name] = data
		} else {
			res.Files[name] = data
		}
	}

	if nd := files[NeutralEntry]; nd != nil {
		if err := unmarshalStrict(nd, &res.Neutral); err != nil {
			return nil, fmt.Errorf("invalid neutral transcript: %w", err)
		}
	}
	return res, nil
}

// VerifyChecksums validates the zip's checksums.json, both directions.
func (r *ReadResult) VerifyChecksums() error {
	data, ok := r.Files[ChecksumsEntry]
	if !ok {
		// v1 bundles predate checksums.
		if r.Manifest.FormatVersion == Version1 {
			return nil
		}
		return ErrChecksumEntryNeeded
	}
	checksums, err := ParseChecksums(data)
	if err != nil {
		return err
	}
	all := map[string][]byte{}
	for k, v := range r.Files {
		all[k] = v
	}
	for k, v := range r.ImageData {
		all[k] = v
	}
	return checksums.Verify(all, ChecksumsEntry)
}

// SessionForAgent returns the raw session bytes when this bundle was
// exported by the given agent.
func (r *ReadResult) SessionForAgent(agent string) ([]byte, bool) {
	if r.Manifest.SourceAgent == agent {
		return r.Session, true
	}
	return nil, false
}

func readZipFiles(entries []*zip.File) (map[string][]byte, error) {
	if len(entries) > maxZipEntries {
		return nil, fmt.Errorf("%w: %d entries exceeds limit %d", ErrArchiveLimit, len(entries), maxZipEntries)
	}

	var declaredTotal uint64
	for _, zf := range entries {
		if zf.UncompressedSize64 > maxZipEntryBytes {
			return nil, fmt.Errorf("%w: entry %q is larger than %d bytes", ErrArchiveLimit, zf.Name, maxZipEntryBytes)
		}
		if zf.UncompressedSize64 > maxZipTotalBytes-declaredTotal {
			return nil, fmt.Errorf("%w: uncompressed content exceeds %d bytes", ErrArchiveLimit, maxZipTotalBytes)
		}
		declaredTotal += zf.UncompressedSize64
	}

	files := make(map[string][]byte, len(entries))
	var actualTotal uint64
	for _, zf := range entries {
		data, err := readZipEntry(zf, maxZipEntryBytes)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", zf.Name, err)
		}
		if uint64(len(data)) > maxZipTotalBytes-actualTotal {
			return nil, fmt.Errorf("%w: uncompressed content exceeds %d bytes", ErrArchiveLimit, maxZipTotalBytes)
		}
		actualTotal += uint64(len(data))
		files[zf.Name] = data
	}
	return files, nil
}

func readZipEntry(zf *zip.File, maxBytes uint64) ([]byte, error) {
	if zf.UncompressedSize64 > maxBytes {
		return nil, fmt.Errorf("%w: entry %q is larger than %d bytes", ErrArchiveLimit, zf.Name, maxBytes)
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return readLimitedZipEntry(rc, zf.Name, maxBytes)
}

func readLimitedZipEntry(r io.Reader, name string, maxBytes uint64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: entry %q exceeds %d bytes while decompressing", ErrArchiveLimit, name, maxBytes)
	}
	return data, nil
}
