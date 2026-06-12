// Package titler turns a session's recent activity into the compact labels
// shown in keen's sidebar: an overall topic, the current task, and a phase
// (planning → done). It shells out to a caller-supplied command — by default
// the claude CLI in print mode with the Haiku model, which reuses the user's
// existing Claude auth, no API key to configure. Callers should treat it as
// best-effort and fall back to a heuristic on error.
package titler

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// maxInputChars caps how much transcript we hand Haiku. A label only needs the
// gist, and the tail (most recent turns) is what tells us where the work is now.
const maxInputChars = 4000

// Summary is the three-part label keen shows for a session.
type Summary struct {
	Topic string // the overall project or goal — fairly stable
	Task  string // what's being worked on right now — changes as work moves
	Phase string // one of: planning, building, testing, shipping, done ("" = unknown)
}

const summaryInstruction = "You are labeling a coding session shown in a compact sidebar. " +
	"Read the recent transcript and reply with EXACTLY three lines, nothing else:\n" +
	"TOPIC: the overall project or goal in 2 to 4 words, lowercase. This is a stable headline — " +
	"keep it steady across the session so the user feels continuity. Only change it if the work has " +
	"clearly moved to a different project or goal, not just a new step within the same one.\n" +
	"TASK: what is being worked on right now in 2 to 5 words, lowercase. This is expected to change " +
	"often as the work moves; make it reflect the latest activity in the transcript.\n" +
	"PHASE: one word, exactly one of: planning building testing shipping done\n" +
	"Use \"done\" only if the work looks finished and it is safe to close the session.\n\n"

// ErrDisabled is returned by Summarize when the titler command is empty —
// the user configured titling off. Callers fall back to a heuristic label.
var ErrDisabled = errors.New("titler disabled by config")

// Summarize asks the titler model to distil recent session text into a
// Summary. command is the argv to run (default: the claude CLI with Haiku —
// see config.Default); the assembled prompt is appended as one final
// argument and stdout is parsed for the three labels. The text is typically
// a transcript tail (see TranscriptTail) but may be a raw prompt. prevTopic
// is the session's current TOPIC (may be empty); passing it lets the model
// keep the headline stable instead of re-deriving — and drifting — every call.
func Summarize(command []string, text, prevTopic string) (Summary, error) {
	if len(command) == 0 {
		return Summary{}, ErrDisabled
	}
	text = strings.TrimSpace(text)
	if len(text) > maxInputChars { // keep the tail — recent context matters most
		text = text[len(text)-maxInputChars:]
	}

	prompt := summaryInstruction
	if prevTopic = strings.TrimSpace(prevTopic); prevTopic != "" {
		prompt += "The session's current TOPIC is \"" + prevTopic + "\". Reuse it verbatim unless " +
			"the work has clearly shifted to a different project or goal.\n\n"
	}
	prompt += "Transcript:\n" + text

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	args := append(append([]string{}, command[1:]...), prompt)
	cmd := exec.CommandContext(ctx, command[0], args...)
	out, err := cmd.Output()
	if err != nil {
		return Summary{}, err
	}
	return parseSummary(string(out)), nil
}

// parseSummary pulls TOPIC/TASK/PHASE out of Haiku's reply, tolerating extra
// lines, missing fields, and case differences in the labels.
func parseSummary(out string) Summary {
	var s Summary
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case hasPrefixFold(line, "TOPIC:"):
			s.Topic = Clean(line[len("TOPIC:"):])
		case hasPrefixFold(line, "TASK:"):
			s.Task = Clean(line[len("TASK:"):])
		case hasPrefixFold(line, "PHASE:"):
			s.Phase = normalizePhase(line[len("PHASE:"):])
		}
	}
	return s
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// phases is the fixed vocabulary, ordered planning → done.
var phases = []string{"planning", "building", "testing", "shipping", "done"}

// normalizePhase maps Haiku's phase word onto the fixed vocabulary, falling
// back to loose synonyms. Returns "" when nothing matches.
func normalizePhase(s string) string {
	s = strings.ToLower(Clean(s))
	for _, p := range phases {
		if strings.Contains(s, p) {
			return p
		}
	}
	switch {
	case containsAny(s, "plan", "design", "explor", "research", "scop", "investigat"):
		return "planning"
	case containsAny(s, "implement", "build", "cod", "writ", "add", "refactor"):
		return "building"
	case containsAny(s, "test", "debug", "fix", "verif"):
		return "testing"
	case containsAny(s, "ship", "merg", "deploy", "release", "commit", "review"):
		return "shipping"
	case containsAny(s, "finish", "complet", "wrap"):
		return "done"
	}
	return ""
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Clean normalizes model (or heuristic) output into a tidy one-line label.
func Clean(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(strings.Trim(s, "\"'`."))
	if len(s) > 40 {
		s = strings.TrimSpace(s[:40])
	}
	return s
}

// Heuristic is the no-LLM fallback for a task label: the first line of the
// prompt, trimmed.
func Heuristic(prompt string) string {
	return Clean(prompt)
}
