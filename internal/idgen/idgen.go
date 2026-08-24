// Package idgen provides identity and time helpers for generated artifacts.
package idgen

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// NewThreadID generates a Codex-style UUIDv7 thread id.
func NewThreadID() string {
	u, err := uuid.NewV7()
	if err != nil {
		// crypto/rand failure is effectively fatal; fall back to time-based.
		return fallbackThreadID()
	}
	return u.String()
}

// NewUUID returns a random UUIDv4 (used for Claude session ids and share ids).
func NewUUID() string {
	return uuid.NewString()
}

func fallbackThreadID() string {
	var b [16]byte
	now := time.Now().UnixNano()
	for i := 0; i < 16; i++ {
		b[i] = byte(now >> (i % 8 * 8))
	}
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

const ImportedTitlePrefix = "[Handoff] "

// ImportedTitle marks imported tasks consistently across target agents.
func ImportedTitle(title, fallback string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = strings.TrimSpace(fallback)
	}
	if strings.HasPrefix(title, ImportedTitlePrefix) {
		return title
	}
	return ImportedTitlePrefix + title
}

// NowRFC3339 returns the current time in RFC3339 with milliseconds.
func NowRFC3339() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// RolloutPath builds the Codex rollout file path convention:
// sessions/YYYY/MM/DD/rollout-<timestamp-with-dashes>-<threadid>.jsonl
func RolloutPath(home, threadID string, t time.Time) string {
	stamp := t.Format("2006-01-02T15-04-05")
	name := fmt.Sprintf("rollout-%s-%s.jsonl", stamp, threadID)
	return filepath.Join(home, "sessions",
		fmt.Sprintf("%04d", t.Year()),
		fmt.Sprintf("%02d", int(t.Month())),
		fmt.Sprintf("%02d", t.Day()),
		name,
	)
}
