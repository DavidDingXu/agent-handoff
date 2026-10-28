package claude

import "strings"

// resumeCommand loads the imported history in its target workspace. Claude
// resumes the UUID across projects but retains the invoking process's cwd.
// The session ID is a locally generated UUID, never source session content.
func resumeCommand(cwd, sessionID, goos string) (command, shell string) {
	if goos == "windows" {
		// PowerShell treats typographic single quotes as delimiters too.
		escapeQuotes := strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’", "‚", "‚‚", "‛", "‛‛")
		quotedCWD := "'" + escapeQuotes.Replace(cwd) + "'"
		return "try { Set-Location -LiteralPath " + quotedCWD + " -ErrorAction Stop; claude --resume " + sessionID + " } catch { Write-Error $_; exit 1 }", "powershell"
	}
	quotedCWD := "'" + strings.ReplaceAll(cwd, "'", "'\"'\"'") + "'"
	return "cd -- " + quotedCWD + " && claude --resume " + sessionID, "sh"
}
