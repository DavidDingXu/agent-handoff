// Command agent-handoff-qoder serves the shared desktop Agent Handoff panel.
package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/cli"
)

const (
	resourceURI  = "ui://agent-handoff/panel.html"
	resourceMIME = "text/html;profile=mcp-app"
)

//go:embed panel.html
var panelHTML string

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "--cli" {
		err = cli.Run(os.Args[2:])
	} else {
		err = runServer(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-handoff:", err)
		os.Exit(1)
	}
}

func runServer(args []string) error {
	flags := flag.NewFlagSet("agent-handoff-ui", flag.ContinueOnError)
	host := flags.String("host", "qoder", "desktop host: qoder or codex")
	browser := flags.Bool("browser", false, "serve the panel in a local browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*host != "qoder" && *host != "codex") {
		return fmt.Errorf("expected --host qoder or --host codex")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "agent-handoff-ui-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	app := newServer(temp, func(ctx context.Context, args ...string) (map[string]any, error) {
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, append([]string{"--cli"}, args...)...)
		data, err := command.Output()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("handoff operation timed out: %w", ctx.Err())
		}
		var result map[string]any
		decodeErr := json.Unmarshal(data, &result)
		if err != nil {
			if decodeErr == nil && result["status"] == "blocked" {
				return result, nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && len(exitErr.Stderr) != 0 {
				return nil, errors.New(string(exitErr.Stderr))
			}
			return nil, err
		}
		return result, decodeErr
	})
	app.host = *host
	if *browser {
		return app.serveBrowser(os.Stdout)
	}
	return app.serve(os.Stdin, os.Stdout)
}

func (s *server) serve(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	writer := json.NewEncoder(output)
	for scanner.Scan() {
		var req request
		reply := response{JSONRPC: "2.0", ID: json.RawMessage("null")}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			reply.Error = &responseError{Code: -32700, Message: "Invalid JSON"}
		} else if req.JSONRPC != "2.0" || req.Method == "" {
			reply.Error = &responseError{Code: -32600, Message: "Invalid request"}
		} else if req.ID == nil {
			continue
		} else {
			reply.ID = req.ID
			result, err := s.handle(context.Background(), req.Method, req.Params)
			if err != nil {
				code := -32602
				if errors.Is(err, errUnknownMethod) {
					code = -32601
				}
				reply.Error = &responseError{Code: code, Message: err.Error()}
			} else {
				reply.Result = result
			}
		}
		if err := writer.Encode(reply); err != nil {
			return err
		}
	}
	return scanner.Err()
}

var errUnknownMethod = errors.New("method not found")

func (s *server) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		var input struct {
			Protocol string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}
		switch input.Protocol {
		case "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25":
		default:
			input.Protocol = "2025-11-25"
		}
		return map[string]any{
			"protocolVersion": input.Protocol,
			"serverInfo":      map[string]any{"name": "agent-handoff", "version": cli.Version},
			"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.tools()}, nil
	case "resources/list":
		return map[string]any{"resources": []any{map[string]any{"uri": resourceURI, "name": "Agent Handoff", "mimeType": resourceMIME}}}, nil
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": []any{}}, nil
	case "resources/read":
		var input struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}
		content, err := s.readResource(input.URI)
		if err != nil {
			return nil, err
		}
		return map[string]any{"contents": []any{content}}, nil
	case "tools/call":
		var input struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}
		result, err := s.callTool(ctx, input.Name, input.Arguments)
		if err != nil {
			return map[string]any{"isError": true, "content": []any{textContent(err.Error())}}, nil
		}
		return map[string]any{"content": []any{textContent("Agent Handoff")}, "structuredContent": result}, nil
	default:
		return nil, errUnknownMethod
	}
}

func textContent(text string) map[string]string {
	return map[string]string{"type": "text", "text": text}
}

func (s *server) readResource(uri string) (map[string]string, error) {
	if uri == resourceURI {
		return map[string]string{"uri": uri, "mimeType": resourceMIME, "text": s.panel("mcp")}, nil
	}
	exportPath := s.exports[uri]
	if exportPath == "" {
		return nil, fmt.Errorf("resource not found")
	}
	data, err := readZIP(exportPath, maxNativeDownloadBytes)
	if err != nil {
		return nil, err
	}
	return map[string]string{"uri": uri, "mimeType": "application/zip", "blob": encodeZIP(data)}, nil
}

func (s *server) panel(transport string) string {
	return strings.NewReplacer(
		"__HANDOFF_VERSION__", cli.Version,
		"__HANDOFF_HOST__", s.host,
		"__HANDOFF_TRANSPORT__", transport,
	).Replace(panelHTML)
}

func (s *server) tools() []map[string]any {
	host := s.host
	label := "Qoder"
	key := "qoder/ui"
	if host == "codex" {
		label = "Codex"
		key = "openai/ui"
	}
	entrypoints := map[string]any{"entrypoints": []any{map[string]string{"type": "global"}, map[string]string{"type": "thread"}}}
	metadata := map[string]any{"ui": map[string]any{"resourceUri": resourceURI}, key: entrypoints}
	if host == "codex" {
		entrypoints["preferredModelDisplayMode"] = "fullscreen"
		metadata["openai/outputTemplate"] = resourceURI
	}
	return []map[string]any{
		{
			"name": "open_handoff", "title": "Agent Handoff",
			"description": "Open the Agent Handoff panel to share a " + label + " task or import a shared task.",
			"inputSchema": inputSchema(nil),
			"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false},
			"_meta":       metadata,
		},
		appTool("list_workspaces", "List usable local workspace directories.", nil, true),
		appTool("upload_handoff_zip", "Receive a ZIP file selected in the panel.", []string{"size", "upload_id", "data"}, false),
		appTool("list_"+host+"_tasks", "Find local "+label+" tasks.", []string{"query"}, true),
		appTool("share_"+host+"_task", "Share the "+label+" task selected in the panel.", []string{"task_id", "format"}, false),
		appTool("preview_handoff", "Preview the local ZIP or encrypted link selected in the panel.", []string{"source", "cwd"}, true),
		appTool("import_"+host+"_task", "Create a "+label+" task from the confirmed preview.", []string{"ticket"}, false),
	}
}

func inputSchema(required []string) map[string]any {
	properties := make(map[string]any, len(required))
	for _, key := range required {
		properties[key] = map[string]string{"type": "string"}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) != 0 {
		schema["required"] = required
	}
	return schema
}

func appTool(name, description string, fields []string, readOnly bool) map[string]any {
	schema := inputSchema(fields)
	if strings.HasPrefix(name, "list_") || name == "upload_handoff_zip" {
		delete(schema, "required")
	}
	return map[string]any{
		"name": name, "description": description, "inputSchema": schema,
		"annotations": map[string]any{"readOnlyHint": readOnly, "destructiveHint": false, "openWorldHint": strings.HasPrefix(name, "share_") || name == "preview_handoff"},
		"_meta":       map[string]any{"ui": map[string]any{"visibility": []string{"app"}}},
	}
}

func (s *server) callTool(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	var input map[string]string
	if len(args) != 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, err
		}
	}
	switch name {
	case "open_handoff":
		return map[string]any{}, nil
	case "upload_handoff_zip":
		return s.upload(input)
	case "list_workspaces":
		return s.run(ctx, s.codexProfileArgs([]string{"workspaces", "--source", s.host})...)
	case "list_" + s.host + "_tasks":
		return s.list(ctx, input["query"])
	case "share_" + s.host + "_task":
		return s.share(ctx, input["task_id"], input["format"])
	case "preview_handoff":
		return s.preview(ctx, input["source"], input["cwd"])
	case "import_" + s.host + "_task":
		return s.importTask(ctx, input["ticket"])
	default:
		return nil, fmt.Errorf("tool not found")
	}
}
