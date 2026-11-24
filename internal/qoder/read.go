package qoder

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/session"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Task identifies a Qoder Desktop task and its workspace.
type Task struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CWD       string `json:"cwd"`
	UpdatedAt string `json:"updated_at"`
}

const desktopTasks = `SELECT session_id, title, cwd, CAST(created_at AS INTEGER), CAST(updated_at AS INTEGER)
	FROM chat_sessions WHERE deleted_at IS NULL AND session_kind = 'standard'`

// List returns the standard Desktop tasks, including archived tasks. Query
// matches a substring of the task's ID, title, or working directory.
func List(dbPath, query string) ([]Task, error) {
	db, err := openTaskCatalog(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(desktopTasks + ` ORDER BY updated_at DESC, session_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read Qoder Desktop task catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()

	query = strings.ToLower(strings.TrimSpace(query))
	tasks := []Task{}
	for rows.Next() {
		task, _, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("read Qoder Desktop task: %w", err)
		}
		if strings.Contains(strings.ToLower(task.ID), query) ||
			strings.Contains(strings.ToLower(task.Title), query) ||
			strings.Contains(strings.ToLower(task.CWD), query) {
			tasks = append(tasks, task)
		}
	}
	return tasks, rows.Err()
}

// Load loads a single Desktop task and derives portable Qoder history and the
// neutral transcript from its selected conversation branch.
func Load(dbPath, runtimeHome, id string) (Task, []byte, neutral.Transcript, error) {
	var empty neutral.Transcript
	parsed, err := uuid.Parse(id)
	if err != nil || !strings.EqualFold(parsed.String(), id) {
		return Task{}, nil, empty, fmt.Errorf("invalid Qoder task ID %q", id)
	}
	db, err := openTaskCatalog(dbPath)
	if err != nil {
		return Task{}, nil, empty, err
	}
	defer func() { _ = db.Close() }()

	task, createdAt, err := scanTask(db.QueryRow(desktopTasks+` AND session_id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, nil, empty, fmt.Errorf("the Qoder Desktop task %s was not found", id)
	}
	if err != nil {
		return Task{}, nil, empty, fmt.Errorf("read Qoder Desktop task: %w", err)
	}
	history, err := readRuntimeHistory(runtimeHome, id)
	if err != nil {
		return Task{}, nil, empty, err
	}
	portable, transcript, err := convertHistory(task, history)
	if err != nil {
		return Task{}, nil, empty, err
	}
	if createdAt != "" {
		transcript.CreatedAt = createdAt
	}
	return task, portable, transcript, nil
}

func openTaskCatalog(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("the Qoder Desktop database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	// The leading slash gives drive-letter paths a file URI of the form
	// file:///C:/... and keeps query characters in the filename escaped.
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
	return sql.Open("sqlite", uri.String())
}

func scanTask(row interface{ Scan(...any) error }) (Task, string, error) {
	var task Task
	var created, updated int64
	if err := row.Scan(&task.ID, &task.Title, &task.CWD, &created, &updated); err != nil {
		return Task{}, "", err
	}
	if strings.TrimSpace(task.Title) == "" {
		task.Title = task.ID
	}
	task.UpdatedAt = sqliteTimestamp(updated)
	return task, sqliteTimestamp(created), nil
}

func sqliteTimestamp(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}

func readRuntimeHistory(runtimeHome, id string) ([]byte, error) {
	if runtimeHome == "" {
		return nil, fmt.Errorf("the Qoder runtime home is required")
	}
	root, err := os.OpenRoot(filepath.Join(runtimeHome, "projects"))
	if err != nil {
		return nil, fmt.Errorf("open Qoder runtime projects: %w", err)
	}
	defer func() { _ = root.Close() }()

	projects, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("list Qoder runtime projects: %w", err)
	}
	var found string
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		candidate := filepath.Join(project.Name(), id+".jsonl")
		info, err := root.Lstat(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("check Qoder runtime history: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("the Qoder runtime history for %s is not a regular file", id)
		}
		if found != "" {
			return nil, fmt.Errorf("multiple Qoder runtime histories found for task %s", id)
		}
		found = candidate
	}
	if found == "" {
		return nil, fmt.Errorf("the Qoder runtime history for task %s was not found", id)
	}
	history, err := root.ReadFile(found)
	if err != nil {
		return nil, fmt.Errorf("read Qoder runtime history: %w", err)
	}
	return history, nil
}

type sourceRecord struct {
	Type           string          `json:"type"`
	UUID           string          `json:"uuid"`
	ParentUUID     string          `json:"parentUuid"`
	SessionID      string          `json:"sessionId"`
	Timestamp      json.RawMessage `json:"timestamp"`
	LeafUUID       string          `json:"leafUuid"`
	Message        *sourceMessage  `json:"message"`
	Meta           bool            `json:"isMeta"`
	Sidechain      bool            `json:"isSidechain"`
	Compact        bool            `json:"isCompactSummary"`
	CompactPrefix  bool            `json:"isCompactionPrefix"`
	TranscriptOnly bool            `json:"isVisibleInTranscriptOnly"`
}

type sourceMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type portableRecord struct {
	Type       string          `json:"type"`
	UUID       string          `json:"uuid"`
	ParentUUID *string         `json:"parentUuid"`
	SessionID  string          `json:"sessionId"`
	Timestamp  string          `json:"timestamp,omitempty"`
	Message    portableMessage `json:"message"`
}

type portableMessage struct {
	Role    string         `json:"role"`
	Content []messageBlock `json:"content"`
}

type messageBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// ToNeutral reads a Qoder transcript or its portable form.
func ToNeutral(task Task, history []byte) (neutral.Transcript, error) {
	_, transcript, err := convertHistory(task, history)
	return transcript, err
}

func activeRecords(id string, history []byte) ([]sourceRecord, error) {
	byID := map[string]sourceRecord{}
	var leaf, lastMessage string
	lineNumber := 0
	for len(history) > 0 {
		line, rest, terminated := bytes.Cut(history, []byte("\n"))
		history = rest
		lineNumber++
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record sourceRecord
		if err := json.Unmarshal(line, &record); err != nil {
			// An active app may be appending the last record during the read.
			if !terminated {
				break
			}
			return nil, fmt.Errorf("invalid Qoder history record on line %d", lineNumber)
		}
		if record.SessionID != "" && record.SessionID != id {
			return nil, fmt.Errorf("the Qoder history contains a different task identity on line %d", lineNumber)
		}
		if record.Type == "active-leaf" {
			leaf = record.LeafUUID
		}
		if record.UUID == "" {
			continue
		}
		if _, exists := byID[record.UUID]; exists {
			return nil, fmt.Errorf("duplicate Qoder history record on line %d", lineNumber)
		}
		byID[record.UUID] = record
		if (record.Type == "user" || record.Type == "assistant") && !record.Sidechain {
			lastMessage = record.UUID
		}
	}
	if leaf == "" {
		leaf = lastMessage
	}
	var branch []sourceRecord
	seen := map[string]bool{}
	for current := leaf; current != ""; {
		record, exists := byID[current]
		if !exists || seen[current] {
			return nil, fmt.Errorf("the Qoder history has an invalid active branch")
		}
		seen[current] = true
		branch = append(branch, record)
		current = record.ParentUUID
	}
	return branch, nil
}

func convertHistory(task Task, history []byte) ([]byte, neutral.Transcript, error) {
	branch, err := activeRecords(task.ID, history)
	if err != nil {
		return nil, neutral.Transcript{}, err
	}
	transcript := neutral.Transcript{
		Schema: neutral.Schema, SourceAgent: "qoder", SourceID: task.ID,
		Title: task.Title, SourceCWD: task.CWD,
	}
	var portable bytes.Buffer
	encoder := json.NewEncoder(&portable)
	encoder.SetEscapeHTML(false)

	calls := map[string]int{}
	var parent *string
	// Parent links run backwards from the active leaf to the first record.
	for i := len(branch) - 1; i >= 0; i-- {
		record := branch[i]
		msg := record.Message
		if msg == nil || record.SessionID != task.ID || record.Type != msg.Role ||
			(record.Type != "user" && record.Type != "assistant") ||
			record.Sidechain || record.Meta || record.Compact || record.CompactPrefix || record.TranscriptOnly {
			continue
		}
		var timestamp string
		_ = json.Unmarshal(record.Timestamp, &timestamp)
		retained := make([]messageBlock, 0)
		for _, block := range parseMessageBlocks(msg.Content) {
			switch {
			case block.Type == "text":
				text := visibleText(msg.Role, block.Text)
				if text == "" {
					continue
				}
				retained = append(retained, messageBlock{Type: "text", Text: text})
				transcript.Entries = append(transcript.Entries, neutral.Entry{
					Kind: neutral.KindMessage, Timestamp: timestamp, Role: msg.Role, Text: text,
				})
			case block.Type == "tool_use" && msg.Role == "assistant" && block.ID != "" && block.Name != "":
				if _, exists := calls[block.ID]; exists {
					continue
				}
				retained = append(retained, messageBlock{Type: "tool_use", ID: block.ID, Name: block.Name, Input: block.Input})
				calls[block.ID] = len(transcript.Entries)
				transcript.Entries = append(transcript.Entries, neutral.Entry{
					Kind: neutral.KindTool, Timestamp: timestamp, Tool: block.Name, Status: "called", Input: compactJSON(block.Input),
				})
			case block.Type == "tool_result" && msg.Role == "user":
				entry, exists := calls[block.ToolUseID]
				if !exists || transcript.Entries[entry].Status != "called" {
					continue
				}
				output := toolOutputText(block.Content)
				data, _ := json.Marshal(output)
				retained = append(retained, messageBlock{Type: "tool_result", ToolUseID: block.ToolUseID, Content: data, IsError: block.IsError})
				transcript.Entries[entry].Output = output
				transcript.Entries[entry].Status = "completed"
				if block.IsError {
					transcript.Entries[entry].Status = "failed"
				}
			}
		}
		if len(retained) == 0 {
			continue
		}
		if transcript.CreatedAt == "" {
			transcript.CreatedAt = timestamp
		}
		if err := encoder.Encode(portableRecord{
			Type: record.Type, UUID: record.UUID, ParentUUID: parent, SessionID: task.ID, Timestamp: timestamp,
			Message: portableMessage{Role: msg.Role, Content: retained},
		}); err != nil {
			return nil, neutral.Transcript{}, fmt.Errorf("encode Qoder portable history: %w", err)
		}
		parent = &record.UUID
	}
	if len(transcript.Entries) == 0 {
		return nil, neutral.Transcript{}, fmt.Errorf("the Qoder task %s has no visible conversation or tool history", task.ID)
	}
	return portable.Bytes(), transcript, nil
}

func parseMessageBlocks(raw json.RawMessage) []messageBlock {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []messageBlock{{Type: "text", Text: text}}
	}
	var blocks []messageBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	return blocks
}

// Qoder can carry harness context from earlier conversations inside a user
// text block, with the user's own message immediately after the wrapper.
var qoderContextTags = func() []*regexp.Regexp {
	var patterns []*regexp.Regexp
	for _, tag := range []string{
		"system-reminder", "in-app-browser-context", "external_browser_tabs",
		"hook_prompt", "task-notification", "external_codex_apps_selected_page_part_[0-9]+_of_[0-9]+",
	} {
		patterns = append(patterns, regexp.MustCompile("(?s)<"+tag+"(?:[[:space:]][^<>]*)?>.*?</"+tag+">"))
	}
	return patterns
}()

func visibleText(role, text string) string {
	if role == "user" && strings.Contains(text, "<") {
		filtered := text
		for _, pattern := range qoderContextTags {
			filtered = pattern.ReplaceAllString(filtered, "")
		}
		if filtered != text {
			text = strings.TrimSpace(filtered)
		}
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || (role == "user" && (session.IsHiddenUserText(trimmed) || strings.HasPrefix(trimmed, "<system-reminder>"))) {
		return ""
	}
	return text
}

func compactJSON(raw json.RawMessage) string {
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return ""
	}
	return compact.String()
}

func toolOutputText(raw json.RawMessage) string {
	var texts []string
	for _, block := range parseMessageBlocks(raw) {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n")
}
