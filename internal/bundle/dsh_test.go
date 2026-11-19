package bundle

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

func TestArchiveRejectsAmbiguousNames(t *testing.T) {
	for _, name := range []string{"manifest.json", "../outside", "/absolute", `C:\absolute`, "a/../b", "a//b", "./manifest.json", `a\b`} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			w, _ := z.Create("manifest.json")
			_, _ = w.Write([]byte(`{}`))
			w, _ = z.Create(name)
			_, _ = w.Write([]byte(`{}`))
			_ = z.Close()
			zr, e := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = readZipFiles(zr.File); e == nil {
				t.Fatal("accepted ambiguous name")
			}
		})
	}
}
func TestDSHNeutralExactLimits(t *testing.T) {
	n := neutral.Transcript{Schema: neutral.Schema, SourceAgent: AgentDSH, SourceID: "id", Entries: []neutral.Entry{{Kind: neutral.KindMessage, Role: "user", Text: "question"}}, Title: strings.Repeat("t", MaxBridgeTitle), SourceCWD: strings.Repeat("c", MaxBridgeIdentity)}
	if e := ValidateDSHNeutral(n, "id"); e != nil {
		t.Fatal(e)
	}
	n.Title += "t"
	if e := ValidateDSHNeutral(n, "id"); e == nil {
		t.Fatal("accepted huge title")
	}
	n.Title = ""
	n.Entries = make([]neutral.Entry, MaxNeutralEntries+1)
	if e := ValidateDSHNeutral(n, "id"); e == nil {
		t.Fatal("accepted excessive entries")
	}
}
