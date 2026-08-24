package claude

import (
	"os"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// ExportData is everything read about a session on the sender side.
type ExportData struct {
	SessionID    string
	Title        string
	CWD          string
	SessionPath  string
	SessionBytes []byte
	IndexEntry   *IndexEntry
	Neutral      neutral.Transcript
}

// LoadSession loads title, cwd and raw session bytes for a session id.
func LoadSession(home, sessionID string) (*ExportData, error) {
	path, err := findSessionPath(home, sessionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entry, _, _ := findIndexEntry(home, sessionID)

	summary := SummarizeSession(data)
	title := summary.Title
	if title == "" && entry != nil && entry.FirstPrompt != "" {
		title = session.Truncate(entry.FirstPrompt, 200)
	}
	if title == "" && summary.FirstUserText != "" {
		title = session.Truncate(summary.FirstUserText, 200)
	}
	if title == "" {
		title = sessionID
	}
	cwd := summary.CWD
	if cwd == "" && entry != nil {
		cwd = entry.ProjectPath
	}
	return &ExportData{
		SessionID:    sessionID,
		Title:        title,
		CWD:          cwd,
		SessionPath:  path,
		SessionBytes: data,
		IndexEntry:   entry,
		Neutral:      neutral.FromClaudeSession(sessionID, title, cwd, data),
	}, nil
}

// Summary is the distilled view of a Claude session.
type Summary struct {
	ID            string
	CWD           string
	Title         string
	FirstUserText string
	LastAgentText string
	CreatedAt     string
	ModifiedAt    string
	GitBranch     string
	MessageCount  int
}

// SummarizeSession scans a Claude session jsonl for summary fields.
func SummarizeSession(data []byte) Summary {
	var s Summary
	s.Title = ""
	session.IterLines(data, func(line session.Line) {
		obj := line.Obj
		if obj == nil {
			return
		}
		ts, _ := obj["timestamp"].(string)
		if ts != "" {
			if s.CreatedAt == "" {
				s.CreatedAt = ts
			}
			s.ModifiedAt = ts
		}
		if branch, _ := obj["gitBranch"].(string); branch != "" {
			s.GitBranch = branch
		}
		if cwd, _ := obj["cwd"].(string); cwd != "" {
			s.CWD = cwd
		}
		isMeta := isTrue(obj["isMeta"])
		sidechain := isTrue(obj["isSidechain"])
		switch session.Type(obj) {
		case "custom-title":
			if v, _ := obj["customTitle"].(string); v != "" {
				s.Title = v
			}
		case "ai-title":
			if v, _ := obj["aiTitle"].(string); v != "" && s.Title == "" {
				s.Title = v
			}
		case "user":
			if isMeta || sidechain {
				return
			}
			if msg, _ := obj["message"].(map[string]any); msg != nil {
				if role, _ := msg["role"].(string); role == "user" {
					if text := claudeText(msg); text != "" && !session.IsHiddenUserText(text) && s.FirstUserText == "" {
						s.FirstUserText = text
					}
				}
			}
			s.MessageCount++
		case "assistant":
			if isMeta || sidechain {
				return
			}
			if msg, _ := obj["message"].(map[string]any); msg != nil {
				if text := claudeText(msg); text != "" {
					s.LastAgentText = text
				}
			}
			s.MessageCount++
		}
	})
	return s
}

func claudeText(msg map[string]any) string {
	if content, ok := msg["content"].(string); ok {
		return content
	}
	parts, _ := msg["content"].([]any)
	var texts []string
	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := pm["type"].(string); t == "text" {
			if txt, _ := pm["text"].(string); txt != "" {
				texts = append(texts, txt)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func isTrue(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
