// Package bundle defines the agent-handoff.zip container format (v2) and its
// reader/writer. A bundle carries one session from one source agent, in
// three representations:
//
//   - the raw native session (lossless for same-agent import)
//   - a neutral transcript (semantic, for cross-agent import)
//   - agent metadata (sqlite row / index entry) for native restore
//
// Layout:
//
//	manifest.json            schema, ids, counts, agent info
//	AGENT_README.md          instructions for a receiving agent
//	<agent>/session.jsonl    raw native session
//	<agent>/meta.json        sender-side metadata (thread row / index entry)
//	<agent>/images.json      image manifest (codex only)
//	<agent>/images/*         image assets (codex only)
//	agent/neutral.json       neutral transcript
//	agent/restore.md         post-import continuation context
//	safety/scan.json         secret scan result at share time
//	checksums.json           sha256 of every other file
//
// v1 bundles (codex-only, flat layout) are accepted for reading.
package bundle

import (
	"fmt"
	"strings"
)

// Format versions.
const (
	Version1 = 1
	Version2 = 2
)

// ArtifactType identifies a agent-handoff zip.
const ArtifactType = "agent-handoff"

// Entry names.
const (
	ManifestEntry   = "manifest.json"
	ReadmeEntry     = "AGENT_README.md"
	ChecksumsEntry  = "checksums.json"
	NeutralEntry    = "agent/neutral.json"
	RestoreEntry    = "agent/restore.md"
	SafetyEntry     = "safety/scan.json"
	SessionEntryFmt = "%s/session.jsonl" // per agent
	MetaEntryFmt    = "%s/meta.json"
	ImagesEntryFmt  = "%s/images.json"
	ImagesPrefixFmt = "%s/images/"
)

// Supported source agents.
const (
	AgentCodex  = "codex"
	AgentClaude = "claude"
)

// Supported import targets.
var SupportedAgents = []string{AgentCodex, AgentClaude}

// Manifest is the bundle's self-description.
type Manifest struct {
	FormatVersion  int      `json:"format_version"`
	ArtifactType   string   `json:"artifact_type"`
	SourceAgent    string   `json:"source_agent"`
	TargetSupport  []string `json:"target_support"`
	SourceThreadID string   `json:"source_thread_id"`
	Title          string   `json:"title"`
	SourceCWD      string   `json:"source_cwd"`
	CreatedAt      string   `json:"created_at"`
	MessageCount   int      `json:"message_count"`
	ImageCount     int      `json:"image_count,omitempty"`
	SourceCLI      string   `json:"source_cli,omitempty"`
	ModelProvider  string   `json:"model_provider,omitempty"`
	GitBranch      string   `json:"git_branch,omitempty"`
	GitOriginURL   string   `json:"git_origin_url,omitempty"`
	Files          []string `json:"files"`
	Notes          []string `json:"notes,omitempty"`
}

// SessionEntry returns the per-agent session entry name.
func SessionEntry(agent string) string { return fmt.Sprintf(SessionEntryFmt, agent) }

// MetaEntry returns the per-agent metadata entry name.
func MetaEntry(agent string) string { return fmt.Sprintf(MetaEntryFmt, agent) }

// ImagesEntry returns the per-agent image manifest entry name.
func ImagesEntry(agent string) string { return fmt.Sprintf(ImagesEntryFmt, agent) }

// ImagesPrefix returns the per-agent image asset prefix.
func ImagesPrefix(agent string) string { return fmt.Sprintf(ImagesPrefixFmt, agent) }

// IsSupportedAgent reports whether the agent name is known.
func IsSupportedAgent(agent string) bool {
	return agent == AgentCodex || agent == AgentClaude
}

// TargetSupported reports whether the manifest declares import support for
// the given target agent.
func (m *Manifest) TargetSupported(target string) bool {
	for _, t := range m.TargetSupport {
		if t == target {
			return true
		}
	}
	return false
}

// SummaryLine renders a one-line human description for logs.
func (m *Manifest) SummaryLine() string {
	return strings.TrimSpace(fmt.Sprintf("%s task %q (%d messages)", m.SourceAgent, m.Title, m.MessageCount))
}
