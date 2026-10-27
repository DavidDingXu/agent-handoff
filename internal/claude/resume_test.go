package claude

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRestoreFallbackRunsInTargetDirectory(t *testing.T) {
	for _, exists := range []bool{true, false} {
		name := "existing target"
		if !exists {
			name = "missing target"
		}
		t.Run(name, func(t *testing.T) {
			start := t.TempDir()
			target := filepath.Join(start, "project ' ‘ ’ ‚ ‛ $(echo SUBSTITUTION) `echo BACKTICK` (one)")
			if exists {
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			res, err := Restore(RestoreInput{SessionBytes: []byte(claudeSrc)}, RestoreOptions{
				Home: t.TempDir(), TargetCWD: target, Execute: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			// The function records invocation without touching any installed
			// Claude client or user configuration. The real shell parses the
			// generated command and performs the directory change.
			shell := "sh"
			args := []string{"-c", "claude() { printf 'CLAUDE_INVOKED\\n'; pwd -P; printf '%s\\n' \"$@\"; }\n" + res.FallbackCmd}
			if runtime.GOOS == "windows" {
				shell = "powershell"
				args = []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); function claude { 'CLAUDE_INVOKED'; (Get-Location).ProviderPath; $args }; " + res.FallbackCmd}
			}
			cmd := exec.Command(shell, args...)
			cmd.Dir = start
			output, err := cmd.CombinedOutput()
			text := strings.ReplaceAll(string(output), "\r\n", "\n")
			if !exists {
				if strings.Contains(text, "CLAUDE_INVOKED") {
					t.Fatalf("Claude started after directory change failed: %s", text)
				}
				if err == nil {
					t.Error("directory change failure returned a successful exit status")
				}
				return
			}
			if err != nil {
				t.Fatalf("resume command failed: %v\n%s", err, text)
			}
			canonicalTarget, err := filepath.EvalSymlinks(target)
			if err != nil {
				t.Fatal(err)
			}
			want := "CLAUDE_INVOKED\n" + canonicalTarget + "\n--resume\n" + res.SessionID + "\n"
			if text != want {
				t.Errorf("resume command output = %q, want %q", text, want)
			}
		})
	}
}
