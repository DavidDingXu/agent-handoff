package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/link"
	"github.com/google/uuid"
)

const (
	ticketLifetime         = 15 * time.Minute
	maxZIPBytes            = 256 << 20
	maxNativeDownloadBytes = 5 << 19 // Base64 plus JSON must fit Qoder’s 4 MiB resource limit.
	maxTaskResults         = 100
)

type cliRunner func(context.Context, ...string) (map[string]any, error)

type server struct {
	host    string
	run     cliRunner
	temp    string
	tickets map[string]previewTicket
	exports map[string]string
	uploads map[string]*fileUpload
}

type previewTicket struct {
	path        string
	cwd         string
	fingerprint string
	created     time.Time
}

func newServer(temp string, run cliRunner) *server {
	return &server{
		host: "qoder", temp: temp, run: run,
		tickets: make(map[string]previewTicket),
		exports: make(map[string]string),
		uploads: make(map[string]*fileUpload),
	}
}

func (s *server) list(ctx context.Context, query string) (map[string]any, error) {
	var args []string
	if s.host == "codex" {
		args = s.codexProfileArgs([]string{"tasks", "--source", "codex"})
	} else {
		args = []string{"qoder", "list", "--query", strings.TrimSpace(query)}
	}
	result, err := s.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	tasks, _ := result["tasks"].([]any)
	if s.host == "codex" {
		query = strings.ToLower(strings.TrimSpace(query))
		if query != "" {
			matches := make([]any, 0, len(tasks))
			for _, value := range tasks {
				task, _ := value.(map[string]any)
				title, _ := task["title"].(string)
				cwd, _ := task["cwd"].(string)
				id, _ := task["id"].(string)
				if strings.Contains(strings.ToLower(title), query) || strings.Contains(strings.ToLower(cwd), query) || strings.Contains(strings.ToLower(id), query) {
					matches = append(matches, value)
				}
			}
			tasks = matches
			result["tasks"] = matches
		}
	}
	if len(tasks) > maxTaskResults {
		result["tasks"] = tasks[:maxTaskResults]
		result["truncated"] = true
	}
	return result, nil
}

func (s *server) share(ctx context.Context, id, format string) (map[string]any, error) {
	if id = strings.TrimSpace(id); id == "" || id == "current" {
		return nil, fmt.Errorf("select a task before sharing")
	}
	if format != "zip" && format != "link" {
		return nil, fmt.Errorf("select ZIP or encrypted link")
	}
	outDir, err := exportDirectory()
	if err != nil {
		return nil, err
	}
	idSuffix := uuid.NewString()
	filename := s.host + "-handoff-" + idSuffix + ".agent-handoff.zip"
	path := filepath.Join(outDir, filename)
	args := s.codexProfileArgs([]string{"share", "--source", s.host, "--thread", id, "--format", format, "--out", path})
	result, err := s.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	if result["path"] == path {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() > maxNativeDownloadBytes {
			return result, nil
		}
		uri := "handoff://agent-handoff/exports/" + filename
		if len(s.exports) >= 20 {
			clear(s.exports)
		}
		s.exports[uri] = path
		result["download_uri"] = uri
	}
	return result, nil
}

func exportDirectory() (string, error) {
	if dir := os.Getenv("AGENT_HANDOFF_EXPORT_DIR"); dir != "" {
		if info, err := os.Stat(dir); !filepath.IsAbs(dir) || err != nil || !info.IsDir() {
			return "", fmt.Errorf("the configured export directory is unavailable")
		}
		return filepath.Clean(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	downloads := filepath.Join(home, "Downloads")
	if info, err := os.Stat(downloads); err == nil && info.IsDir() {
		return downloads, nil
	}
	return home, nil
}

func (s *server) preview(ctx context.Context, source, cwd string) (map[string]any, error) {
	now := time.Now()
	s.expireTickets(now)
	if source = strings.TrimSpace(source); source == "" {
		return nil, fmt.Errorf("enter a local ZIP path or encrypted link")
	}
	cwd = strings.TrimSpace(cwd)
	if !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("enter the absolute path of an existing workspace directory")
	}
	cwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil, fmt.Errorf("workspace directory does not exist")
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("workspace directory does not exist")
	}
	var data []byte
	if strings.HasPrefix(source, uploadPrefix) {
		path, uploadErr := s.uploadedZIP(source, now)
		if uploadErr != nil {
			return nil, uploadErr
		}
		data, err = readZIP(path, maxZIPBytes)
	} else if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		data, _, err = link.DownloadURL(source, nil)
	} else if filepath.IsAbs(source) && strings.HasSuffix(strings.ToLower(source), ".zip") {
		data, err = readZIP(source, maxZIPBytes)
	} else {
		return nil, fmt.Errorf("enter an absolute ZIP path or an encrypted handoff link")
	}
	// Provider errors may contain signed URLs. Keep the supplied link out of the response.
	if err != nil {
		if strings.HasPrefix(source, "http") {
			return nil, fmt.Errorf("could not open the encrypted handoff link")
		}
		return nil, fmt.Errorf("could not read the handoff ZIP: %w", err)
	}
	if len(s.tickets) >= 8 {
		return nil, fmt.Errorf("too many active previews; try again after a previous preview expires")
	}
	ticket := uuid.NewString()
	path := filepath.Join(s.temp, ticket+".agent-handoff.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("could not save the handoff preview: %w", err)
	}
	result, err := s.run(ctx, "preview", path)
	fingerprint, _ := result["fingerprint"].(string)
	if err != nil || result["checksum_status"] != "ok" || fingerprint == "" {
		_ = os.Remove(path)
		if err != nil {
			return nil, err
		}
		if result["checksum_status"] == "not_available_v1" {
			return nil, fmt.Errorf("this legacy handoff ZIP has no checksum; export it again with a current Agent Handoff")
		}
		return nil, fmt.Errorf("handoff integrity check failed")
	}
	s.tickets[ticket] = previewTicket{path: path, cwd: cwd, fingerprint: fingerprint, created: now}
	preview := map[string]any{
		"ticket": ticket, "cwd": cwd,
		"checksum_status": "ok", "message_count": result["message_count"],
	}
	for _, key := range []string{"title", "source_agent", "first_user_message", "last_message"} {
		if value, ok := result[key].(string); ok {
			text := []rune(value)
			if len(text) > 1600 {
				value = string(text[:1600]) + "…"
			}
			preview[key] = value
		}
	}
	return preview, nil
}

func (s *server) importTask(ctx context.Context, key string) (map[string]any, error) {
	s.expireTickets(time.Now())
	ticket, ok := s.tickets[key]
	if !ok {
		return nil, fmt.Errorf("preview this handoff again before importing")
	}
	delete(s.tickets, key)
	defer os.Remove(ticket.path)
	args := s.codexProfileArgs([]string{"import", ticket.path, "--target", s.host, "--cwd", ticket.cwd, "--expected-fingerprint", ticket.fingerprint, "--execute"})
	return s.run(ctx, args...)
}

func (s *server) codexProfileArgs(args []string) []string {
	if s.host == "codex" {
		if home := os.Getenv("AGENT_HANDOFF_CODEX_PROFILE"); home != "" {
			return append(args, "--home", home)
		}
	}
	return args
}

func (s *server) expireTickets(now time.Time) {
	for key, ticket := range s.tickets {
		if now.Sub(ticket.created) >= ticketLifetime {
			_ = os.Remove(ticket.path)
			delete(s.tickets, key)
		}
	}
}

func readZIP(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("ZIP must be a regular file no larger than %d bytes", limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		return nil, fmt.Errorf("ZIP exceeds %d bytes", limit)
	}
	return data, err
}

func encodeZIP(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
