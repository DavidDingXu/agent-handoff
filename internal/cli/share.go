package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/claude"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/images"
	"github.com/DavidDingXu/agent-handoff/internal/link"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/safety"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

func cmdShare(args []string) error {
	fs := flag.NewFlagSet("share", flag.ExitOnError)
	source := sourceFlag(fs)
	thread := fs.String("thread", "current", "thread/session id, or current")
	home := fs.String("home", "", "agent home (defaults to CODEX_HOME/~/.codex or CLAUDE_CONFIG_DIR/~/.claude)")
	out := fs.String("out", "", "output zip path (defaults to ./<slug>.agent-handoff.zip)")
	format := fs.String("format", "zip", "export format: zip or link")
	endpoint := fs.String("endpoint", "", "optional self-hosted share service endpoint for --format link")
	token := fs.String("token", "", "share service bearer token")
	ttl := fs.Int("ttl", 0, "link lifetime in seconds (anonymous: 60-3600; self-hosted: 60-86400; default 600)")
	includeSecrets := fs.Bool("include-secrets", false, "proceed even when the secret scan finds high-confidence secrets")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sourceAgent, err := detectSource(*source)
	if err != nil {
		return err
	}

	var manifest *bundle.Manifest
	var sessionBytes []byte
	var meta map[string]any
	var imgs []bundle.ImageAsset
	var imageData map[string][]byte

	switch sourceAgent {
	case bundle.AgentCodex:
		manifest, sessionBytes, meta, imgs, imageData, err = exportCodex(*home, *thread)
	case bundle.AgentClaude:
		manifest, sessionBytes, meta, imgs, imageData, err = exportClaude(*home, *thread)
	}
	if err != nil {
		return err
	}

	result := map[string]any{
		"source_agent":  manifest.SourceAgent,
		"thread_id":     manifest.SourceThreadID,
		"title":         manifest.Title,
		"source_cwd":    manifest.SourceCWD,
		"message_count": manifest.MessageCount,
	}

	findings, _ := sessionFindings(manifest, sessionBytes)
	if len(findings) > 0 && !*includeSecrets {
		result["status"] = "blocked"
		result["safety"] = map[string]any{"status": "blocked", "findings": findings}
		_ = printJSON(result)
		return fmt.Errorf("secret scan found %d finding(s); re-run with --include-secrets only if this is intentional", len(findings))
	}

	writerInput := bundle.WriterInput{
		Manifest:  manifest,
		Session:   sessionBytes,
		Meta:      meta,
		Images:    imgs,
		ImageData: imageData,
		Neutral:   neutralFrom(manifest, sessionBytes),
	}
	zipPath := *out
	if zipPath == "" {
		zipPath = bundle.DefaultSharePath(manifest.Title)
	}

	switch *format {
	case "zip":
		if err := bundle.WriteZip(zipPath, writerInput); err != nil {
			return err
		}
		st, _ := os.Stat(zipPath)
		result["status"] = "ok"
		result["safety"] = map[string]any{"status": safety.Status(findings), "findings": findings}
		result["path"] = absolutePath(zipPath)
		result["size_bytes"] = st.Size()
		result["image_count"] = bundle.CountCopied(imgs)
		result["image_missing"] = bundle.CountMissing(imgs)
		return printJSON(result)

	case "link":
		ep := link.ResolveEndpoint(*endpoint)
		explicitEndpoint := link.HasExplicitEndpoint(*endpoint)
		payload, err := bundle.WriteZipBytes(writerInput)
		if err != nil {
			return err
		}

		var serviceWarning string
		if ep != "" {
			shareURL, m, uploadErr := link.Upload(payload, manifest.SourceThreadID, manifest.Title, link.UploadOptions{
				Endpoint:   ep,
				Token:      link.ResolveToken(*token),
				TTLSeconds: *ttl,
			})
			if uploadErr == nil {
				result["status"] = "ok"
				result["safety"] = map[string]any{"status": safety.Status(findings), "findings": findings}
				result["share_url"] = shareURL
				if m.ExpiresAt != "" {
					result["expires_at"] = m.ExpiresAt
				}
				result["note"] = "the key lives in the #k= fragment; the server never sees it — send the full link"
				return printJSON(result)
			}
			if explicitEndpoint {
				return writeZipFallback(result, zipPath, writerInput, uploadErr.Error())
			}
			serviceWarning = "project service: " + uploadErr.Error()
		}

		res, relayErr := link.UploadRelay(payload, link.RelayUploadOptions{TTLSeconds: *ttl})
		if relayErr != nil {
			reason := relayErr.Error()
			if serviceWarning != "" {
				reason = serviceWarning + "; " + reason
			}
			return writeZipFallback(result, zipPath, writerInput, reason)
		}
		result["status"] = "ok"
		result["safety"] = map[string]any{"status": safety.Status(findings), "findings": findings}
		result["share_url"] = res.ShareURL
		result["expires_at"] = res.ExpiresAt
		result["providers"] = res.Providers
		result["replica_count"] = len(res.Providers)
		warnings := res.ProviderErrors
		if serviceWarning != "" {
			warnings = append([]string{serviceWarning}, warnings...)
		}
		if len(warnings) > 0 {
			result["provider_warnings"] = warnings
		}
		result["note"] = "the encrypted bundle is mirrored by anonymous providers; the key and replica addresses live only in the #h= fragment — send the full link"
		return printJSON(result)

	default:
		return fmt.Errorf("unsupported --format %q (expected zip|link)", *format)
	}
}

func writeZipFallback(result map[string]any, zipPath string, in bundle.WriterInput, reason string) error {
	if err := bundle.WriteZip(zipPath, in); err != nil {
		return err
	}
	if err := addZipFallback(result, zipPath, reason); err != nil {
		return err
	}
	return printJSON(result)
}

func addZipFallback(result map[string]any, zipPath, reason string) error {
	st, err := os.Stat(zipPath)
	if err != nil {
		return err
	}
	result["status"] = "fallback_zip"
	result["fallback"] = reason
	result["path"] = absolutePath(zipPath)
	result["size_bytes"] = st.Size()
	return nil
}

func sessionFindings(m *bundle.Manifest, sessionBytes []byte) ([]safety.Finding, error) {
	if m.SourceAgent == bundle.AgentClaude {
		var findings []safety.Finding
		session.IterLines(sessionBytes, func(line session.Line) {
			if line.Obj == nil {
				return
			}
			if f := safety.ScanPlain(line.Raw); len(f) > 0 {
				findings = append(findings, f...)
			}
		})
		return findings, nil
	}
	return safety.Scan(sessionBytes), nil
}

func neutralFrom(m *bundle.Manifest, sessionBytes []byte) neutral.Transcript {
	if m.SourceAgent == bundle.AgentCodex {
		return neutral.FromCodexSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	}
	return neutral.FromClaudeSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
}

func exportCodex(homeFlag, threadFlag string) (*bundle.Manifest, []byte, map[string]any, []bundle.ImageAsset, map[string][]byte, error) {
	home, err := codex.ResolveHome(homeFlag)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	threadID, err := codex.ResolveThread(home, threadFlag)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	data, err := codex.LoadThread(home, threadID)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Normalize: drop rolled-back turns and the trailing self-export turn so
	// the receiver sees exactly what the sender sees.
	normalized := codex.NormalizeActiveSession(data.SessionBytes, threadID)
	normalized = codex.DropSelfExportTurn(normalized)

	imgs, imageData, err := images.Collect(normalized, data.CWD)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	m := &bundle.Manifest{
		FormatVersion:  bundle.Version2,
		ArtifactType:   bundle.ArtifactType,
		SourceAgent:    bundle.AgentCodex,
		TargetSupport:  []string{bundle.AgentCodex, bundle.AgentClaude},
		SourceThreadID: threadID,
		Title:          data.Title,
		SourceCWD:      data.CWD,
		CreatedAt:      idgen.NowRFC3339(),
		MessageCount:   session.CountUserAssistantMessages(normalized),
		ImageCount:     bundle.CountCopied(imgs),
	}
	if row := data.ThreadRow; row != nil {
		if v, ok := row["cli_version"].(string); ok {
			m.SourceCLI = v
		}
		if v, ok := row["model_provider"].(string); ok {
			m.ModelProvider = v
		}
		if v, ok := row["git_branch"].(string); ok {
			m.GitBranch = v
		}
		if v, ok := row["git_origin_url"].(string); ok {
			m.GitOriginURL = v
		}
	}
	return m, normalized, data.ThreadRow, imgs, imageData, nil
}

func exportClaude(homeFlag, sessionFlag string) (*bundle.Manifest, []byte, map[string]any, []bundle.ImageAsset, map[string][]byte, error) {
	home, err := claude.ResolveHome(homeFlag)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	sessionID, err := claude.ResolveSession(home, sessionFlag)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	data, err := claude.LoadSession(home, sessionID)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	meta := map[string]any{}
	if data.IndexEntry != nil {
		meta["index_entry"] = *data.IndexEntry
	}
	summary := claude.SummarizeSession(data.SessionBytes)

	m := &bundle.Manifest{
		FormatVersion:  bundle.Version2,
		ArtifactType:   bundle.ArtifactType,
		SourceAgent:    bundle.AgentClaude,
		TargetSupport:  []string{bundle.AgentClaude, bundle.AgentCodex},
		SourceThreadID: sessionID,
		Title:          data.Title,
		SourceCWD:      data.CWD,
		CreatedAt:      idgen.NowRFC3339(),
		MessageCount:   summary.MessageCount,
		SourceCLI:      "claude-code",
		ModelProvider:  "anthropic",
		GitBranch:      summary.GitBranch,
	}
	return m, data.SessionBytes, meta, nil, nil, nil
}
