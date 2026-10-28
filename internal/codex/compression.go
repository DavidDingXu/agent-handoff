package codex

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Codex can replace an idle rollout with its .zst sibling while retaining
// the canonical .jsonl path in SQLite. Prefer the plain file when both exist,
// including when the caller discovered or stored the compressed path.
func readRollout(path string) ([]byte, error) {
	plainPath := plainRolloutPath(path)
	data, err := os.ReadFile(plainPath)
	if err == nil || !errors.Is(err, os.ErrNotExist) || !strings.HasSuffix(plainPath, ".jsonl") {
		return data, err
	}

	compressedPath := plainPath + ".zst"
	f, err := os.Open(compressedPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	decoder, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, fmt.Errorf("decompress %s: %w", compressedPath, err)
	}
	defer decoder.Close()
	data, err = io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("decompress %s: %w", compressedPath, err)
	}
	return data, nil
}

func plainRolloutPath(path string) string {
	if strings.HasSuffix(path, ".jsonl.zst") {
		return strings.TrimSuffix(path, ".zst")
	}
	return path
}

func rolloutExists(path string) bool {
	plainPath := plainRolloutPath(path)
	return fileExists(plainPath) || (strings.HasSuffix(plainPath, ".jsonl") && fileExists(plainPath+".zst"))
}
