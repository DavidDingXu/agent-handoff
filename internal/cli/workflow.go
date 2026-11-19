package cli

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/DavidDingXu/agent-handoff/internal/brief"
	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/ledger"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/safety"
	"github.com/DavidDingXu/agent-handoff/internal/workspace"
)

type pathFlags []string

func (p *pathFlags) String() string     { return fmt.Sprint([]string(*p)) }
func (p *pathFlags) Set(s string) error { *p = append(*p, s); return nil }

func codeFindings(p *workspace.Payload) []safety.Finding {
	entries, err := p.Entries()
	if err != nil {
		return nil
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var findings []safety.Finding
	for _, key := range keys {
		for _, f := range safety.ScanPlain(string(entries[key])) {
			path := key
			if len(path) > 512 || len(safety.ScanPlain(path)) > 0 {
				path = "code/<selected-file>"
			}
			findings = append(findings, safety.Finding{Rule: f.Rule, Path: path})
		}
	}
	return findings
}

// Optional reviewed content participates in identity without changing the legacy
// session-only fingerprint of ordinary, automatically summarized handoffs.
func handoffFingerprint(b *bundle.ReadResult) string {
	data := b.Session
	if b.Code != nil || b.Brief != nil && b.Brief.Notes != "" {
		h := sha256.New()
		_, _ = h.Write(data)
		if b.Code != nil {
			raw, _ := json.Marshal(b.Code)
			_, _ = h.Write([]byte("\x00code\x00"))
			_, _ = h.Write(raw)
		}
		if b.Brief != nil && b.Brief.Notes != "" {
			_, _ = h.Write([]byte("\x00notes\x00"))
			_, _ = h.Write([]byte(b.Brief.Notes))
		}
		data = h.Sum(nil)
	}
	return ledger.Fingerprint(b.Manifest.SourceAgent, b.Manifest.SourceThreadID, data)
}

func bridgeWorkflow(operation string, args []string) (any, error) {
	fs := flag.NewFlagSet(operation, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	input := fs.String("input", "-", "JSON input file or stdin")
	cwd := fs.String("cwd", "", "local repository directory")
	out := fs.String("out", "", "new independent worktree directory")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return nil, bridgeFail("invalid_input")
	}
	if operation == "code-inspect" {
		v, e := workspace.Inspect(*cwd)
		if e != nil {
			return nil, bridgeFail("code_unavailable")
		}
		return v, nil
	}
	var r io.Reader = os.Stdin
	if *input != "-" {
		f, e := os.Open(*input)
		if e != nil {
			return nil, bridgeFail("read_failed")
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	data, e := io.ReadAll(io.LimitReader(r, bundle.MaxBridgeRequest+1))
	if e != nil || len(data) > bundle.MaxBridgeRequest {
		return nil, bridgeFail("payload_too_large")
	}
	switch operation {
	case "brief":
		var t neutral.Transcript
		if bundle.DecodeBridgeJSON(data, &t) != nil || bundle.ValidateReadNeutral(t) != nil {
			return nil, bridgeFail("invalid_neutral")
		}
		return brief.Generate(t), nil
	case "code-capture":
		var paths []string
		if bundle.DecodeBridgeJSON(data, &paths) != nil {
			return nil, bridgeFail("invalid_input")
		}
		v, e := workspace.Capture(*cwd, paths)
		if e != nil {
			return nil, bridgeFail("code_capture_failed")
		}
		return v, nil
	case "code-apply":
		var p workspace.Payload
		if bundle.DecodeBridgeJSON(data, &p) != nil || workspace.Validate(&p) != nil {
			return nil, bridgeFail("invalid_code")
		}
		if !filepath.IsAbs(*cwd) || !filepath.IsAbs(*out) {
			return nil, bridgeFail("invalid_input")
		}
		if e := workspace.Apply(*cwd, *out, &p); e != nil {
			return nil, bridgeFail("code_apply_failed")
		}
		return map[string]any{"status": "ok", "directory": *out}, nil
	}
	return nil, bridgeFail("invalid_input")
}
