package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// withGlobal points XDG_CONFIG_HOME at a temp dir and writes keen/config.json
// there when content is non-empty. Returns the temp dir.
func withGlobal(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if content != "" {
		keenDir := filepath.Join(dir, "keen")
		if err := os.MkdirAll(keenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(keenDir, "config.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// withLocal writes .keen.json into a fresh temp cwd and returns that cwd.
func withLocal(t *testing.T, content string) string {
	t.Helper()
	cwd := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(cwd, LocalFile), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return cwd
}

func TestDefaultsWhenNoFiles(t *testing.T) {
	withGlobal(t, "")
	cfg, err := Load(withLocal(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.SessionCommand, []string{"claude", "--permission-mode", "auto"}) {
		t.Errorf("SessionCommand = %v", cfg.SessionCommand)
	}
	if !reflect.DeepEqual(cfg.TasksCommand, []string{"bd", "list", "--json"}) {
		t.Errorf("TasksCommand = %v", cfg.TasksCommand)
	}
	if cfg.TasksLabel != "bd list" {
		t.Errorf("TasksLabel = %q", cfg.TasksLabel)
	}
	if !reflect.DeepEqual(cfg.TitlerCommand, []string{"claude", "--model", "haiku", "-p"}) {
		t.Errorf("TitlerCommand = %v", cfg.TitlerCommand)
	}
}

func TestGlobalFileOverridesDefaults(t *testing.T) {
	withGlobal(t, `{"session_command": ["codex"], "tasks_command": ["my-tasks", "--json"]}`)
	cfg, err := Load(withLocal(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.SessionCommand, []string{"codex"}) {
		t.Errorf("SessionCommand = %v", cfg.SessionCommand)
	}
	if !reflect.DeepEqual(cfg.TasksCommand, []string{"my-tasks", "--json"}) {
		t.Errorf("TasksCommand = %v", cfg.TasksCommand)
	}
	// Untouched fields keep their defaults.
	if !reflect.DeepEqual(cfg.TitlerCommand, []string{"claude", "--model", "haiku", "-p"}) {
		t.Errorf("TitlerCommand = %v", cfg.TitlerCommand)
	}
}

func TestLocalOverridesGlobalPerField(t *testing.T) {
	withGlobal(t, `{"session_command": ["codex"], "tasks_label": "global label"}`)
	cwd := withLocal(t, `{"tasks_label": "local label"}`)
	cfg, err := Load(cwd)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TasksLabel != "local label" {
		t.Errorf("TasksLabel = %q, want local override", cfg.TasksLabel)
	}
	if !reflect.DeepEqual(cfg.SessionCommand, []string{"codex"}) {
		t.Errorf("SessionCommand = %v, want global value to survive", cfg.SessionCommand)
	}
}

func TestEmptyArrayDisables(t *testing.T) {
	withGlobal(t, `{"tasks_command": [], "titler_command": []}`)
	cfg, err := Load(withLocal(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.TasksCommand) != 0 || cfg.TasksCommand == nil {
		t.Errorf("TasksCommand = %#v, want explicit empty (disabled)", cfg.TasksCommand)
	}
	if len(cfg.TitlerCommand) != 0 || cfg.TitlerCommand == nil {
		t.Errorf("TitlerCommand = %#v, want explicit empty (disabled)", cfg.TitlerCommand)
	}
	// Defaults still fill what wasn't mentioned.
	if !reflect.DeepEqual(cfg.SessionCommand, []string{"claude", "--permission-mode", "auto"}) {
		t.Errorf("SessionCommand = %v", cfg.SessionCommand)
	}
}

func TestTasksLabelDerivedFromCommand(t *testing.T) {
	withGlobal(t, `{"tasks_command": ["jira", "issues", "--mine", "--json"]}`)
	cfg, err := Load(withLocal(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TasksLabel != "jira issues" {
		t.Errorf("TasksLabel = %q, want derived %q", cfg.TasksLabel, "jira issues")
	}
}

func TestMalformedFileErrors(t *testing.T) {
	withGlobal(t, `{not json`)
	if _, err := Load(withLocal(t, "")); err == nil {
		t.Fatal("Load: want error for malformed global config")
	}
	withGlobal(t, "")
	if _, err := Load(withLocal(t, `{"session_command": "not-an-array"}`)); err == nil {
		t.Fatal("Load: want error for malformed local config")
	}
}
