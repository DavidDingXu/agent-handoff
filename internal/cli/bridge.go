package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/ledger"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/safety"
)

const bridgeShareSchema = "agent-handoff.bridge.share.v1"

type bridgeShareRequest struct {
	Schema         string             `json:"schema"`
	SourceThreadID string             `json:"source_thread_id"`
	Title          string             `json:"title"`
	SourceCWD      string             `json:"source_cwd"`
	SourceCLI      string             `json:"source_cli"`
	Native         json.RawMessage    `json:"native"`
	Neutral        neutral.Transcript `json:"neutral"`
}
type bridgeFailure struct {
	code     string
	findings []safety.Finding
}

func (e *bridgeFailure) Error() string { return "bridge operation failed: " + e.code }
func bridgeFail(code string) error     { return &bridgeFailure{code: code} }

// cmdBridge buffers every response, including errors: stdout is exactly one
// bounded JSON object, never a progress log or a partial native payload.
func cmdBridge(args []string) error {
	var result any
	var err error
	if len(args) == 0 {
		err = bridgeFail("invalid_input")
	} else {
		switch args[0] {
		case "share":
			result, err = bridgeShare(args[1:])
		case "read":
			result, err = bridgeRead(args[1:])
		default:
			err = bridgeFail("invalid_input")
		}
	}
	if err == nil {
		data, e := json.Marshal(result)
		if e != nil || len(data)+1 > bundle.MaxBridgeResponse {
			err = bridgeFail("payload_too_large")
		} else {
			_, e = os.Stdout.Write(append(data, '\n'))
			return e
		}
	}
	var failure *bridgeFailure
	if !errors.As(err, &failure) {
		failure = &bridgeFailure{code: "operation_failed"}
	}
	out := map[string]any{"status": "error", "error_code": failure.code, "message": "Bridge request failed (" + failure.code + ")."}
	if len(failure.findings) > 0 {
		out["status"] = "blocked"
		out["safety"] = map[string]any{"status": "blocked", "findings": failure.findings}
	}
	_ = printJSON(out)
	return failure
}

func bridgeShare(args []string) (any, error) {
	fs := flag.NewFlagSet("bridge share", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	input := fs.String("input", "", "request file or - for stdin")
	out := fs.String("out", "", "output zip")
	format := fs.String("format", "zip", "zip or link")
	endpoint := fs.String("endpoint", "", "share endpoint")
	token := fs.String("token", "", "share token")
	ttl := fs.Int("ttl", 0, "link lifetime")
	config := fs.String("config", "", "provider config")
	include := fs.Bool("include-secrets", false, "explicit secret override")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *input == "" || (*format != "zip" && *format != "link") {
		return nil, bridgeFail("invalid_input")
	}
	var r io.Reader = os.Stdin
	if *input != "-" {
		f, err := os.Open(*input)
		if err != nil {
			return nil, bridgeFail("read_failed")
		}
		defer func() { _ = f.Close() }()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			return nil, bridgeFail("invalid_input")
		}
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, bundle.MaxBridgeRequest+1))
	if err != nil {
		return nil, bridgeFail("read_failed")
	}
	if len(data) > bundle.MaxBridgeRequest {
		return nil, bridgeFail("payload_too_large")
	}
	var req bridgeShareRequest
	if bundle.DecodeBridgeJSON(data, &req) != nil {
		return nil, bridgeFail("invalid_input")
	}
	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(data, &rawFields)
	if len(rawFields["neutral"]) > bundle.MaxBridgeNeutral {
		return nil, bridgeFail("payload_too_large")
	}
	if req.Schema != bridgeShareSchema {
		return nil, bridgeFail("unsupported_schema")
	}
	if strings.TrimSpace(req.SourceThreadID) == "" || strings.TrimSpace(req.Title) == "" {
		return nil, bridgeFail("invalid_input")
	}
	if len(req.SourceThreadID) > bundle.MaxBridgeIdentity || len(req.Title) > bundle.MaxBridgeTitle || len(req.SourceCWD) > bundle.MaxBridgeIdentity || len(req.SourceCLI) > bundle.MaxBridgeIdentity || len(req.Native) > bundle.MaxDSHNative {
		return nil, bridgeFail("payload_too_large")
	}
	if _, err = bundle.ParseDSHNative(req.Native, req.SourceThreadID); err != nil {
		return nil, bridgeFail("invalid_native")
	}
	if err = bundle.ValidateDSHNeutral(req.Neutral, req.SourceThreadID); err != nil {
		return nil, bridgeFail("invalid_neutral")
	}
	// Decode the complete input independently of typed fields, scanning both
	// string values and property names, including SDK extension data.
	var decoded any
	_ = json.Unmarshal(data, &decoded)
	findings := bridgeScan(decoded, "$")
	if len(findings) > 0 && !*include {
		return nil, &bridgeFailure{code: "secret_detected", findings: findings}
	}
	count := 0
	for _, e := range req.Neutral.Entries {
		if e.Kind == neutral.KindMessage {
			count++
		}
	}
	m := &bundle.Manifest{FormatVersion: bundle.Version2, ArtifactType: bundle.ArtifactType, SourceAgent: bundle.AgentDSH, SourceThreadID: req.SourceThreadID, Title: req.Title, SourceCWD: req.SourceCWD, SourceCLI: req.SourceCLI, CreatedAt: idgen.NowRFC3339(), MessageCount: count, TargetSupport: []string{bundle.AgentCodex, bundle.AgentClaude, bundle.AgentDSH}}
	// A compact single native envelope is valid JSONL; preserve all SDK data.
	var compact bytes.Buffer
	_ = json.Compact(&compact, req.Native)
	native := append(compact.Bytes(), '\n')
	result := map[string]any{"schema": "agent-handoff.bridge.share-result.v1", "source_agent": bundle.AgentDSH, "thread_id": m.SourceThreadID, "title": m.Title, "source_cwd": m.SourceCWD, "message_count": count, "safety": map[string]any{"status": safety.Status(findings), "findings": findings}}
	var response any
	err = sharePrepared(bundle.WriterInput{Manifest: m, Session: native, Neutral: req.Neutral}, result, findings, out, format, endpoint, token, config, ttl, func(v any) error { response = v; return nil })
	if err != nil {
		return nil, bridgeFail("share_failed")
	}
	// Remote errors and local paths are not allowed to smuggle credentials into
	// diagnostics. Detailed provider failures remain an existing CLI contract.
	if _, ok := result["fallback"]; ok {
		result["fallback"] = "Link upload unavailable; local zip is ready."
	}
	if _, ok := result["provider_warnings"]; ok {
		result["provider_warnings"] = []string{"One or more upload providers were unavailable."}
	}
	return response, nil
}

func bridgeScan(v any, path string) []safety.Finding {
	// A shared deeply nested prefix must not expand every finding unboundedly.
	if len(path) > 1024 {
		path = path[:1024]
		for !utf8.ValidString(path) {
			path = path[:len(path)-1]
		}
		path += "..."
	}
	var out []safety.Finding
	switch x := v.(type) {
	case string:
		for _, f := range safety.ScanPlain(x) {
			out = append(out, safety.Finding{Rule: f.Rule, Path: path})
		}
	case []any:
		for i, e := range x {
			out = append(out, bridgeScan(e, fmt.Sprintf("%s[%d]", path, i))...)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			safe := k
			if len(k) > 80 || len(safety.ScanPlain(k)) > 0 || strings.ContainsAny(k, "\r\n\t") {
				safe = "<field>"
			}
			next := path + "." + safe
			out = append(out, bridgeScan(k, next+".<key>")...)
			out = append(out, bridgeScan(x[k], next)...)
		}
	}
	// Findings are diagnostics, not a lossless source representation. Bound them
	// so a malicious request cannot grow the error response beyond its wire cap.
	if len(out) > 1000 {
		return out[:1000]
	}
	return out
}

func bridgeRead(args []string) (any, error) {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return nil, bridgeFail("invalid_input")
	}
	loaded, err := loadBundleArg(args[0])
	if err != nil {
		return nil, bridgeFail("read_failed")
	}
	b := loaded.Bundle
	if err = b.VerifyChecksums(); err != nil {
		return nil, bridgeFail("integrity_failed")
	}
	nativeSize := len(b.Session)
	if b.Manifest.SourceAgent == bundle.AgentDSH {
		nativeSize = len(bytes.TrimSpace(b.Session))
	}
	if nativeSize > bundle.MaxDSHNative || !utf8.Valid(b.Session) {
		return nil, bridgeFail("payload_too_large")
	}
	// Keep legacy manifest extension fields compatible while rejecting hidden
	// trailing documents, duplicate keys, and invalid UTF-8 at this boundary.
	if len(b.Manifest.Title) > bundle.MaxBridgeTitle || len(b.Manifest.SourceThreadID) > bundle.MaxBridgeIdentity || len(b.Manifest.SourceCWD) > bundle.MaxBridgeIdentity || len(b.Manifest.SourceCLI) > bundle.MaxBridgeIdentity {
		return nil, bridgeFail("payload_too_large")
	}
	var manifestFields map[string]json.RawMessage
	if bundle.DecodeBridgeJSON(b.Files[bundle.ManifestEntry], &manifestFields) != nil {
		return nil, bridgeFail("invalid_input")
	}
	if raw := b.Files[bundle.NeutralEntry]; raw != nil {
		if len(raw) > bundle.MaxBridgeNeutral {
			return nil, bridgeFail("payload_too_large")
		}
		if bundle.DecodeBridgeJSON(raw, &b.Neutral) != nil {
			return nil, bridgeFail("invalid_neutral")
		}
	}
	var native *bundle.DSHNative
	if b.Manifest.SourceAgent == bundle.AgentDSH {
		if err = bundle.ValidateDSHBundle(b); err != nil {
			return nil, bridgeFail("invalid_native")
		}
		native, _ = bundle.ParseDSHNative(b.Session, b.Manifest.SourceThreadID)
	} else if b.Neutral.Schema == "" {
		b.Neutral = neutralFrom(b.Manifest, b.Session)
	}
	nd, err := json.Marshal(b.Neutral)
	if err != nil || len(nd) > bundle.MaxBridgeNeutral || len(b.Neutral.Entries) > bundle.MaxNeutralEntries {
		return nil, bridgeFail("payload_too_large")
	}
	if b.Neutral.Schema != neutral.Schema || b.Neutral.SourceAgent != b.Manifest.SourceAgent || b.Neutral.SourceID != b.Manifest.SourceThreadID {
		return nil, bridgeFail("invalid_neutral")
	}
	if bundle.ValidateReadNeutral(b.Neutral) != nil {
		return nil, bridgeFail("invalid_neutral")
	}
	checksum := "ok"
	if b.Manifest.FormatVersion == bundle.Version1 && b.Files[bundle.ChecksumsEntry] == nil {
		checksum = "not_available_v1"
	}
	warnings := []string{}
	if b.Manifest.ImageCount > 0 || len(b.Images) > 0 {
		warnings = append(warnings, "Images and attachments may not survive semantic import.")
	}
	return map[string]any{"schema": "agent-handoff.bridge.read-result.v1", "status": "ok", "checksum_status": checksum, "fingerprint": ledger.Fingerprint(b.Manifest.SourceAgent, b.Manifest.SourceThreadID, b.Session), "manifest": b.Manifest, "native": native, "neutral": b.Neutral, "source_session": string(b.Session), "warnings": warnings}, nil
}
