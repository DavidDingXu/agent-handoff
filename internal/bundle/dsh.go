package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

const (
	MaxBridgeRequest  = 32 << 20
	MaxBridgeResponse = 32 << 20
	MaxDSHNative      = 16 << 20
	MaxBridgeNeutral  = 8 << 20
	MaxNeutralEntries = 100000
	MaxBridgeTitle    = 4 << 10
	MaxBridgeIdentity = 8 << 10
	DSHNativeSchema   = "agent-handoff.dsh.native.v1"
)

// DSHNative is inert SDK data. The pinned plugin validates the SDK vocabulary.
type DSHNative struct {
	Schema               string                       `json:"schema"`
	RuntimeVersion       string                       `json:"runtime_version"`
	SessionFormatVersion int                          `json:"session_format_version"`
	Header               map[string]json.RawMessage   `json:"header"`
	InheritedEventCount  int                          `json:"inherited_event_count"`
	Events               []map[string]json.RawMessage `json:"events"`
}

// DecodeBridgeJSON rejects unknown fields, duplicate object keys, invalid UTF-8,
// and trailing JSON. Raw SDK header/events remain data, not Go configuration.
func DecodeBridgeJSON(data []byte, dst any) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueJSON(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	return nil
}
func uniqueJSON(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return fmt.Errorf("duplicate JSON key")
			}
			seen[s] = true
			if err := uniqueJSON(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSON(d); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, err = d.Token()
	return err
}

func ParseDSHNative(data []byte, sourceID string) (*DSHNative, error) {
	if len(bytes.TrimSpace(data)) > MaxDSHNative {
		return nil, fmt.Errorf("native payload exceeds limit")
	}
	var fields map[string]json.RawMessage
	if err := DecodeBridgeJSON(data, &fields); err != nil {
		return nil, fmt.Errorf("invalid native envelope")
	}
	for _, key := range []string{"schema", "runtime_version", "session_format_version", "header", "inherited_event_count", "events"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return nil, fmt.Errorf("missing native field")
		}
	}
	var n DSHNative
	if err := DecodeBridgeJSON(data, &n); err != nil {
		return nil, fmt.Errorf("invalid native envelope")
	}
	if n.Schema != DSHNativeSchema || strings.TrimSpace(n.RuntimeVersion) == "" || len(n.RuntimeVersion) > MaxBridgeIdentity || n.SessionFormatVersion <= 0 || n.Header == nil || n.InheritedEventCount < 0 || len(n.Events) == 0 {
		return nil, fmt.Errorf("invalid native envelope")
	}
	var id string
	if json.Unmarshal(n.Header["id"], &id) != nil || id != sourceID {
		return nil, fmt.Errorf("native identity mismatch")
	}
	for _, e := range n.Events {
		if len(e) == 0 {
			return nil, fmt.Errorf("invalid native event")
		}
	}
	return &n, nil
}

// ValidateDSHNeutral is shared by the bridge reader and native CLI imports.
func ValidateDSHNeutral(t neutral.Transcript, sourceID string) error {
	if t.Schema != neutral.Schema || t.SourceAgent != AgentDSH || t.SourceID != sourceID || len(t.Entries) == 0 || len(t.Entries) > MaxNeutralEntries {
		return fmt.Errorf("invalid neutral identity or entries")
	}
	if len(t.Title) > MaxBridgeTitle || len(t.SourceCWD) > MaxBridgeIdentity {
		return fmt.Errorf("neutral metadata exceeds limit")
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil || len(data) > MaxBridgeNeutral {
		return fmt.Errorf("neutral payload exceeds limit")
	}
	return validateNeutralEntries(t, true)
}

// ValidateReadNeutral accepts an empty legacy transcript, but never accepts
// unsupported entry kinds, executable status values, or authority roles.
func ValidateReadNeutral(t neutral.Transcript) error {
	if t.Schema != neutral.Schema || len(t.Entries) > MaxNeutralEntries || len(t.Title) > MaxBridgeTitle || len(t.SourceID) > MaxBridgeIdentity || len(t.SourceCWD) > MaxBridgeIdentity {
		return fmt.Errorf("invalid neutral transcript")
	}
	return validateNeutralEntries(t, false)
}

func validateNeutralEntries(t neutral.Transcript, requireUser bool) error {
	users := 0
	for _, e := range t.Entries {
		switch e.Kind {
		case neutral.KindMessage:
			if (e.Role != "user" && e.Role != "assistant") || strings.TrimSpace(e.Text) == "" || e.Tool != "" || e.Status != "" || e.Input != "" || e.Output != "" {
				return fmt.Errorf("invalid neutral message")
			}
			if e.Role == "user" {
				users++
			}
		case neutral.KindTool:
			if strings.TrimSpace(e.Tool) == "" || (e.Status != "called" && e.Status != "completed" && e.Status != "failed") || e.Role != "" || e.Text != "" {
				return fmt.Errorf("invalid neutral tool")
			}
		default:
			return fmt.Errorf("invalid neutral kind")
		}
	}
	if requireUser && users == 0 {
		return fmt.Errorf("neutral requires a user message")
	}
	return nil
}

func ValidateDSHBundle(b *ReadResult) error {
	m := b.Manifest
	if m.SourceAgent != AgentDSH {
		return nil
	}
	if m.FormatVersion != Version2 || strings.TrimSpace(m.SourceThreadID) == "" || len(m.SourceThreadID) > MaxBridgeIdentity || strings.TrimSpace(m.Title) == "" || len(m.Title) > MaxBridgeTitle || len(m.SourceCWD) > MaxBridgeIdentity {
		return fmt.Errorf("invalid DSH manifest")
	}
	// DSH v1 exports have no attachments or imported runtime configuration.
	if len(b.Images) > 0 || len(b.ImageData) > 0 || len(b.Meta) > 0 {
		return fmt.Errorf("unsupported DSH metadata or attachments")
	}
	if raw := b.Files[MetaEntry(AgentDSH)]; raw != nil {
		var meta map[string]json.RawMessage
		if DecodeBridgeJSON(raw, &meta) != nil || len(meta) > 0 {
			return fmt.Errorf("invalid DSH metadata")
		}
	}
	if raw := b.Files[NeutralEntry]; raw != nil {
		if len(raw) > MaxBridgeNeutral || DecodeBridgeJSON(raw, &b.Neutral) != nil {
			return fmt.Errorf("invalid DSH neutral")
		}
	}
	if _, err := ParseDSHNative(b.Session, m.SourceThreadID); err != nil {
		return err
	}
	return ValidateDSHNeutral(b.Neutral, m.SourceThreadID)
}
