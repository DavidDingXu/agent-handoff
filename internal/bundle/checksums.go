package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Checksums maps zip entry name to hex sha256.
type Checksums map[string]string

// checksumsFile is the on-disk representation.
type checksumsFile struct {
	Algorithm string            `json:"algorithm"`
	Files     map[string]string `json:"files"`
}

// BuildChecksums computes sha256 for every entry (checksums.json itself
// excluded by construction: it is computed over the payload map before the
// entry is written).
func BuildChecksums(files map[string][]byte) Checksums {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	out := Checksums{}
	for _, name := range names {
		sum := sha256.Sum256(files[name])
		out[name] = hex.EncodeToString(sum[:])
	}
	return out
}

// Marshal renders the checksums entry payload.
func (c Checksums) Marshal() []byte {
	if c == nil {
		c = Checksums{}
	}
	data, _ := json.MarshalIndent(checksumsFile{Algorithm: "sha256", Files: c}, "", "  ")
	return append(data, '\n')
}

// ParseChecksums decodes a checksums.json payload.
func ParseChecksums(data []byte) (Checksums, error) {
	var f checksumsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Algorithm != "sha256" {
		return nil, ErrChecksumAlgorithm
	}
	return f.Files, nil
}

// Verify checks both directions: every listed file exists with matching hash,
// and every file in the zip is listed. skip lists entries exempt from the
// coverage check (checksums.json itself).
func (c Checksums) Verify(files map[string][]byte, skip ...string) error {
	exempt := map[string]bool{}
	for _, s := range skip {
		exempt[s] = true
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for name, want := range c {
		data, ok := files[name]
		if !ok {
			return &VerifyError{Entry: name, Reason: "checksum references missing file"}
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			return &VerifyError{Entry: name, Reason: "checksum mismatch"}
		}
	}
	for _, name := range names {
		if exempt[name] {
			continue
		}
		if _, ok := c[name]; !ok {
			return &VerifyError{Entry: name, Reason: "file not covered by checksums"}
		}
	}
	return nil
}

// VerifyError describes a checksum verification failure.
type VerifyError struct {
	Entry  string
	Reason string
}

func (e *VerifyError) Error() string { return e.Reason + ": " + e.Entry }
