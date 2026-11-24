package qoder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

type restoreEntry struct {
	Role      string
	Parts     []string
	Timestamp string
}

type restoreMessage struct {
	ID            string
	TurnID        string
	Role          string
	Timestamp     time.Time
	TurnStartedAt *int64
	Payload       []byte
}

type restorePart struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// Preserve visible roles and put historical tools into ordinary text blocks.
// Tool arguments and output are evidence and carry no runtime actions.
func restoreConversation(t neutral.Transcript) []restoreEntry {
	var entries []restoreEntry
	for _, entry := range t.Entries {
		switch entry.Kind {
		case neutral.KindMessage:
			if (entry.Role == "user" || entry.Role == "assistant") && entry.Text != "" {
				entries = append(entries, restoreEntry{entry.Role, []string{entry.Text}, entry.Timestamp})
			}
		case neutral.KindTool:
			text := fmt.Sprintf("[Historical tool] %s (%s)", entry.Tool, entry.Status)
			if entry.Input != "" {
				text += "\nInput: " + entry.Input
			}
			if entry.Output != "" {
				text += "\nOutput: " + entry.Output
			}
			if len(entries) == 0 || entries[len(entries)-1].Role != "assistant" {
				entries = append(entries, restoreEntry{"assistant", nil, entry.Timestamp})
			}
			entries[len(entries)-1].Parts = append(entries[len(entries)-1].Parts, text)
		}
	}
	// An assistant reply and its preceding tool evidence belong to one turn.
	var joined []restoreEntry
	for _, entry := range entries {
		if len(joined) > 0 && entry.Role == "assistant" && joined[len(joined)-1].Role == "assistant" {
			last := &joined[len(joined)-1]
			last.Parts = append(last.Parts, entry.Parts...)
		} else {
			joined = append(joined, entry)
		}
	}
	return joined
}

// The portable Qoder history keeps the original message grouping. Use the
// source parser again because handoff payloads are always untrusted.
func restoreNativeConversation(id string, history []byte) ([]restoreEntry, error) {
	portable, _, err := convertHistory(Task{ID: id}, history)
	if err != nil {
		return nil, fmt.Errorf("prepare Qoder native history: %w", err)
	}
	var entries []restoreEntry
	var calls = make(map[string]string)
	decoder := json.NewDecoder(bytes.NewReader(portable))
	for decoder.More() {
		var record portableRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, err
		}
		entry := restoreEntry{Role: record.Message.Role, Timestamp: record.Timestamp}
		toolResults := restoreEntry{Role: "assistant", Timestamp: record.Timestamp}
		for _, block := range record.Message.Content {
			switch block.Type {
			case "text":
				entry.Parts = append(entry.Parts, block.Text)
			case "tool_use":
				calls[block.ID] = block.Name
				entry.Parts = append(entry.Parts, "[Historical tool call] "+block.Name+"\nInput: "+compactJSON(block.Input))
			case "tool_result":
				name := calls[block.ToolUseID]
				status := "completed"
				if block.IsError {
					status = "failed"
				}
				toolResults.Parts = append(toolResults.Parts, "[Historical tool result] "+name+" ("+status+")\nOutput: "+toolOutputText(block.Content))
			}
		}
		// Qoder wraps tool results in a user record. Attach them to the
		// preceding turn and let genuine user text start the next one.
		if len(toolResults.Parts) > 0 {
			entries = append(entries, toolResults)
		}
		if len(entry.Parts) > 0 {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func restoreRuntimePath(home, cwd, id string) string {
	// QoderJS sanitizes UTF-16 code units, then adds a base-36 hash to
	// project directory names whose sanitized form exceeds 200 bytes.
	const maxPrefix = 200
	units := utf16.Encode([]rune(cwd))
	prefix := make([]byte, 0, min(len(units), maxPrefix))
	hash := int32(5381)
	for _, unit := range units {
		hash = hash*33 ^ int32(unit)
		if len(prefix) == maxPrefix {
			continue
		}
		switch {
		case unit >= 'a' && unit <= 'z', unit >= 'A' && unit <= 'Z', unit >= '0' && unit <= '9':
			prefix = append(prefix, byte(unit))
		default:
			prefix = append(prefix, '-')
		}
	}
	if len(units) > maxPrefix {
		absoluteHash := int64(hash)
		if absoluteHash < 0 {
			absoluteHash = -absoluteHash
		}
		prefix = append(prefix, '-')
		prefix = strconv.AppendInt(prefix, absoluteHash, 36)
	}
	return filepath.Join(home, "projects", string(prefix), id+".jsonl")
}

func buildRestoreHistory(entries []restoreEntry, result RestoreResult, now time.Time) ([]byte, []restoreMessage, error) {
	var history bytes.Buffer
	encoder := json.NewEncoder(&history)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(map[string]any{
		"type": "workspace-directories", "sessionId": result.SessionID, "directories": []string{result.CWD},
	}); err != nil {
		return nil, nil, err
	}
	messages := make([]restoreMessage, 0, len(entries))
	var parentID any
	var turnID string
	var turnStartedAt int64
	var lastPrompt string
	for index, entry := range entries {
		eventID := idgen.NewUUID()
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			ts = now.Add(time.Duration(index) * time.Millisecond)
		}
		stamp := ts.UTC().Format("2006-01-02T15:04:05.000Z")
		if turnID == "" || entry.Role == "user" {
			turnID = eventID
			turnStartedAt = ts.UnixMilli()
		}

		nativeParts := make([]restorePart, 0, len(entry.Parts))
		displayParts := make([]restorePart, 0, len(entry.Parts))
		for i, text := range entry.Parts {
			nativeParts = append(nativeParts, restorePart{Type: "text", Text: text})
			displayParts = append(displayParts, restorePart{
				ID: fmt.Sprintf("%s:text:%d", eventID, i), Type: "text", Text: text,
			})
		}
		native := map[string]any{"role": entry.Role, "content": nativeParts}
		display := map[string]any{"parts": displayParts}
		projection := restoreMessage{ID: eventID, TurnID: turnID, Role: entry.Role, Timestamp: ts}
		if entry.Role == "assistant" {
			providerID := "msg_" + strings.ReplaceAll(eventID, "-", "")
			native["id"] = providerID
			native["type"] = "message"
			native["model"] = "auto"
			native["stop_reason"] = "end_turn"
			native["stop_sequence"] = nil
			display["completedAt"] = stamp
			display["forkBoundaryProviderId"] = providerID
			projection.ID = eventID + ":assistant"
			projection.TurnStartedAt = new(turnStartedAt)
		} else {
			lastPrompt = strings.Join(entry.Parts, "\n")
		}
		projection.Payload, err = json.Marshal(display)
		if err != nil {
			return nil, nil, err
		}
		messages = append(messages, projection)

		if err := encoder.Encode(map[string]any{
			"type": entry.Role, "uuid": eventID, "parentUuid": parentID, "timestamp": stamp,
			"sessionId": result.SessionID, "cwd": result.CWD, "isSidechain": false,
			"userType": "external", "entrypoint": "cli", "message": native,
		}); err != nil {
			return nil, nil, err
		}
		parentID = eventID
	}
	if err := encoder.Encode(map[string]any{
		"type": "active-leaf", "sessionId": result.SessionID, "leafUuid": parentID,
		"explicit": true, "timestamp": now.Format("2006-01-02T15:04:05.000Z"),
	}); err != nil {
		return nil, nil, err
	}
	if err := encoder.Encode(map[string]any{
		"type": "custom-title", "sessionId": result.SessionID, "customTitle": result.Title,
	}); err != nil {
		return nil, nil, err
	}
	if lastPrompt != "" {
		if err := encoder.Encode(map[string]any{
			"type": "last-prompt", "sessionId": result.SessionID, "lastPrompt": lastPrompt,
		}); err != nil {
			return nil, nil, err
		}
	}
	return history.Bytes(), messages, nil
}

func publishRestoreHistory(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".handoff-history-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// A hard link atomically publishes the completed file and rejects an
	// existing path. Both paths are in the same directory.
	return os.Link(file.Name(), path)
}
