package cli

import (
	"flag"
	"fmt"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/link"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

func cmdPreview(args []string) error {
	fs := flag.NewFlagSet("preview", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: agent-handoff preview FILE.agent-handoff.zip|URL")
	}

	loaded, err := loadBundleArg(fs.Arg(0))
	if err != nil {
		return err
	}
	b := loaded.Bundle

	detail := extractDetail(b)
	result := map[string]any{
		"status":             "ok",
		"source_agent":       b.Manifest.SourceAgent,
		"title":              b.Manifest.Title,
		"source_cwd":         b.Manifest.SourceCWD,
		"source_thread":      b.Manifest.SourceThreadID,
		"created_at":         b.Manifest.CreatedAt,
		"message_count":      b.Manifest.MessageCount,
		"image_count":        bundle.CountCopied(b.Images),
		"first_user_message": detail.FirstUserMessage,
		"last_message":       detail.LastMessage,
		"session_start":      detail.FirstTimestamp,
		"session_end":        detail.LastTimestamp,
	}
	for _, k := range []string{"git_branch", "git_origin_url", "cli_version", "model_provider"} {
		switch k {
		case "git_branch":
			if b.Manifest.GitBranch != "" {
				result[k] = b.Manifest.GitBranch
			}
		case "git_origin_url":
			if b.Manifest.GitOriginURL != "" {
				result[k] = b.Manifest.GitOriginURL
			}
		case "cli_version":
			if b.Manifest.SourceCLI != "" {
				result[k] = b.Manifest.SourceCLI
			}
		case "model_provider":
			if b.Manifest.ModelProvider != "" {
				result[k] = b.Manifest.ModelProvider
			}
		}
	}
	if b.Manifest.FormatVersion == bundle.Version2 {
		if err := b.VerifyChecksums(); err != nil {
			result["checksum_status"] = fmt.Sprintf("failed: %v", err)
		} else {
			result["checksum_status"] = "ok"
		}
	}
	return printJSON(result)
}

// extractDetail summarizes either a codex or claude session payload.
func extractDetail(b *bundle.ReadResult) session.Detail {
	if b.Manifest.SourceAgent == bundle.AgentClaude {
		return claudeDetail(b.Session)
	}
	return session.ExtractDetail(b.Session)
}

// loadedBundle pairs a bundle with its source address (file path or URL).
type loadedBundle struct {
	Bundle *bundle.ReadResult
	Path   string // zip path for file sources; "" for URL sources
	URL    string // cleaned share URL for link sources
}

// loadBundleArg loads a bundle from a file path or a share link URL.
func loadBundleArg(arg string) (*loadedBundle, error) {
	if isURL(arg) {
		payload, cleanURL, err := link.DownloadURL(arg, nil)
		if err != nil {
			return nil, err
		}
		b, err := bundle.ReadZipBytes(payload)
		if err != nil {
			return nil, err
		}
		return &loadedBundle{Bundle: b, URL: cleanURL}, nil
	}
	b, err := bundle.ReadZip(arg)
	if err != nil {
		return nil, err
	}
	return &loadedBundle{Bundle: b, Path: arg}, nil
}
