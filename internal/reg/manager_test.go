package reg

import "testing"

// scanLine builds a scanFormat line the way tmux would expand it.
func scanLine(fields ...string) string {
	out := ""
	for i, f := range fields {
		if i > 0 {
			out += fieldSep
		}
		out += f
	}
	return out
}

func TestParseScanLine(t *testing.T) {
	s, win, ok := parseScanLine(scanLine(
		"%3", "@1", "kabc123", "2", "quonfig", "main", "/Users/x/quonfig",
		"crunching", "keen tmux spike", "rototilling the engine", "building",
		"123456", "/tmp/transcript.jsonl",
	))
	if !ok {
		t.Fatal("parseScanLine rejected a valid line")
	}
	if s.PaneID != "%3" || win != "@1" || s.ID != "kabc123" || s.Seq != 2 {
		t.Errorf("identity fields wrong: pane=%s win=%s id=%s seq=%d", s.PaneID, win, s.ID, s.Seq)
	}
	if s.Status != StatusCrunching || s.Topic != "keen tmux spike" || s.ContextTokens != 123456 {
		t.Errorf("state fields wrong: status=%v topic=%q tokens=%d", s.Status, s.Topic, s.ContextTokens)
	}
}

func TestParseScanLineSkipsNonSessions(t *testing.T) {
	// The sidebar pane (and any hand-made pane) has no @keen-session option —
	// tmux expands it to "". Scan must skip it.
	_, _, ok := parseScanLine(scanLine(
		"%0", "@1", "", "", "", "", "", "", "", "", "", "", "",
	))
	if ok {
		t.Fatal("parseScanLine accepted a pane with no @keen-session")
	}
}

func TestSortBySeqAndNextSeq(t *testing.T) {
	m := &Manager{sessions: []*Session{{ID: "b", Seq: 5}, {ID: "a", Seq: 1}, {ID: "c", Seq: 3}}}
	sortBySeq(m.sessions)
	got := m.sessions[0].ID + m.sessions[1].ID + m.sessions[2].ID
	if got != "acb" {
		t.Errorf("sortBySeq order = %s, want acb", got)
	}
	if n := m.nextSeq(); n != 6 {
		t.Errorf("nextSeq = %d, want 6 (max+1, never reusing a freed seq)", n)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	for st := StatusStarting; st <= StatusExited; st++ {
		if got := ParseStatus(st.String()); got != st {
			t.Errorf("ParseStatus(%q) = %v, want %v", st.String(), got, st)
		}
	}
	if got := ParseStatus("garbage"); got != StatusStarting {
		t.Errorf("ParseStatus(garbage) = %v, want StatusStarting", got)
	}
}
