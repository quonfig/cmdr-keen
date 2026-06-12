package titler

import (
	"os"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trims surrounding whitespace", "  fix login bug  ", "fix login bug"},
		{"keeps only the first line", "add vt parsing\nblah blah", "add vt parsing"},
		{"strips wrapping quotes", "\"refactor auth\"", "refactor auth"},
		{"strips backticks and trailing period", "`update deps`.", "update deps"},
		{"collapses to empty on whitespace only", "   \n  ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clean(tt.in); got != tt.want {
				t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCleanTruncates(t *testing.T) {
	long := strings.Repeat("x", 100)
	got := Clean(long)
	if len([]rune(got)) > 40 {
		t.Errorf("Clean did not cap length: got %d runes", len([]rune(got)))
	}
}

func TestHeuristic(t *testing.T) {
	// Heuristic is the no-LLM fallback: first line of the prompt, cleaned.
	in := "Implement the parser\nand then write tests"
	if got, want := Heuristic(in), "Implement the parser"; got != want {
		t.Errorf("Heuristic() = %q, want %q", got, want)
	}
}

func TestParseSummary(t *testing.T) {
	out := "TOPIC: keen sidebar\nTASK: fix mouse scroll\nPHASE: building\n"
	s := parseSummary(out)
	if s.Topic != "keen sidebar" || s.Task != "fix mouse scroll" || s.Phase != "building" {
		t.Errorf("parseSummary = %+v", s)
	}
}

func TestParseSummaryTolerant(t *testing.T) {
	// Extra prose, lowercase labels, missing fields, and a phase synonym.
	out := "Sure! Here you go:\ntopic: the parser\nphase: just shipping it now\n"
	s := parseSummary(out)
	if s.Topic != "the parser" {
		t.Errorf("Topic = %q, want %q", s.Topic, "the parser")
	}
	if s.Task != "" {
		t.Errorf("Task = %q, want empty", s.Task)
	}
	if s.Phase != "shipping" {
		t.Errorf("Phase = %q, want %q", s.Phase, "shipping")
	}
}

func TestNormalizePhase(t *testing.T) {
	tests := map[string]string{
		"planning":           "planning",
		"  Building ":        "building",
		"test":               "testing",
		"we are debugging":   "testing",
		"merging the branch": "shipping",
		"all wrapped up":     "done",
		"":                   "",
		"vibes":              "",
	}
	for in, want := range tests {
		if got := normalizePhase(in); got != want {
			t.Errorf("normalizePhase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTranscriptTail(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/transcript.jsonl"
	// A string-content user prompt, an array-content assistant turn (text +
	// tool_use — only the text should survive), and a tool_result user turn
	// (no text blocks — should be dropped entirely).
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"fix the mouse scroll"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"On it."},{"type":"tool_use","name":"Edit","input":{}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := TranscriptTail(path, 0)
	want := "user: fix the mouse scroll\nassistant: On it."
	if got != want {
		t.Errorf("TranscriptTail = %q, want %q", got, want)
	}

	if TranscriptTail(dir+"/missing.jsonl", 0) != "" {
		t.Errorf("TranscriptTail on missing file should be empty")
	}
}

func TestSummarizeDisabled(t *testing.T) {
	if _, err := Summarize(nil, "some transcript", ""); err != ErrDisabled {
		t.Errorf("Summarize(nil cmd) err = %v, want ErrDisabled", err)
	}
}

func TestSummarizeCustomCommand(t *testing.T) {
	// A stand-in titler: ignores the appended prompt ($0 swallows it) and
	// prints a fixed three-line reply, proving argv order and parsing.
	cmd := []string{"sh", "-c", `printf 'TOPIC: keen config\nTASK: wiring tests\nPHASE: testing\n'`}
	sum, err := Summarize(cmd, "transcript text", "")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Topic != "keen config" || sum.Task != "wiring tests" || sum.Phase != "testing" {
		t.Errorf("Summarize = %+v", sum)
	}
}
