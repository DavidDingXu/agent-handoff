package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
)

func workflowRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.name", "Synthetic"}, {"config", "user.email", "synthetic@example.test"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, b)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "hello.txt"}, {"commit", "-m", "synthetic base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestWorkflowBridgeBriefNotesAndSelectedCode(t *testing.T) {
	root := workflowRepo(t)
	var request map[string]any
	_ = json.Unmarshal([]byte(bridgeFixture()), &request)
	request["source_cwd"] = root
	request["brief_note"] = "Review the text change before continuing."
	request["code_files"] = []string{"hello.txt"}
	data, _ := json.Marshal(request)
	path := filepath.Join(t.TempDir(), "workflow.zip")
	result, err := runBridgeTest(t, "bridge", "share", "--input", writeBridgeInput(t, string(data)), "--out", path)
	if err != nil {
		t.Fatalf("enhanced share: %v %#v", err, result)
	}
	read, err := runBridgeTest(t, "bridge", "read", path)
	if err != nil {
		t.Fatal(err)
	}
	if read["brief"] == nil || read["code"] == nil {
		t.Fatalf("missing review payloads: %#v", read)
	}
	b, err := bundle.ReadZip(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.VerifyChecksums(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b.Files["agent/brief.json"]), "Review the text change") || len(b.Files["code/changes.patch"]) == 0 {
		t.Fatal("bundle omitted notes or selected code")
	}
}

func TestWorkflowBridgeInspectCodeAndBrief(t *testing.T) {
	root := workflowRepo(t)
	result, err := runBridgeTest(t, "bridge", "code-inspect", "--cwd", root)
	if err != nil || result["files"] == nil {
		t.Fatalf("code preview missing: %#v %v", result, err)
	}
	var request map[string]any
	_ = json.Unmarshal([]byte(bridgeFixture()), &request)
	data, _ := json.Marshal(request["neutral"])
	result, err = runBridgeTest(t, "bridge", "brief", "--input", writeBridgeInput(t, string(data)))
	if err != nil || result["objective"] == nil {
		t.Fatalf("brief missing: %#v %v", result, err)
	}
}

func TestWorkflowBriefNoteSecretsBlockBeforeZip(t *testing.T) {
	var request map[string]any
	_ = json.Unmarshal([]byte(bridgeFixture()), &request)
	request["brief_note"] = "ghp_" + strings.Repeat("X", 36)
	data, _ := json.Marshal(request)
	path := filepath.Join(t.TempDir(), "blocked.zip")
	result, err := runBridgeTest(t, "bridge", "share", "--input", writeBridgeInput(t, string(data)), "--out", path)
	if err == nil || result["status"] != "blocked" {
		t.Fatalf("note must be scanned: %#v %v", result, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("blocked content produced a zip")
	}
}

func TestWorkflowBridgeCodeFindingsNeverEchoPrivateMaterial(t *testing.T) {
	root := workflowRepo(t)
	secretName := "ghp_" + strings.Repeat("Y", 36) + ".txt"
	secret := "-----BEGIN PRIVATE KEY-----\nSYNTHETIC-PRIVATE-MATERIAL\n-----END PRIVATE KEY-----\n"
	if e := os.WriteFile(filepath.Join(root, secretName), []byte(secret), 0600); e != nil {
		t.Fatal(e)
	}
	var request map[string]any
	_ = json.Unmarshal([]byte(bridgeFixture()), &request)
	request["source_cwd"] = root
	request["code_files"] = []string{secretName}
	data, _ := json.Marshal(request)
	result, err := runBridgeTest(t, "bridge", "share", "--input", writeBridgeInput(t, string(data)), "--out", filepath.Join(t.TempDir(), "blocked.zip"))
	if err == nil || result["status"] != "blocked" {
		t.Fatalf("expected sensitive code block: %#v %v", result, err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "SYNTHETIC-PRIVATE") || strings.Contains(string(encoded), secretName) {
		t.Fatalf("findings leaked code or secret filename: %s", encoded)
	}
}
