// Package config loads keen's optional configuration file. keen works with
// zero config — every field has a default aimed at Claude Code + bd — but each
// hardcoded command can be swapped out: what a session runs, what fills the
// tasks section, and what model call labels sessions.
//
// Two files are consulted, both optional:
//
//   - global: $XDG_CONFIG_HOME/keen/config.json (~/.config/keen/config.json)
//   - local:  .keen.json in the directory keen was launched from
//
// Local fields override global fields one by one; anything neither file sets
// falls back to the default. An explicit empty array ([]) is not "unset" — it
// disables that feature (no tasks section, no LLM titling).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocalFile is the per-directory override file, looked up in the directory
// keen was launched from.
const LocalFile = ".keen.json"

// Config is keen's resolved configuration: every field is filled in (defaults
// applied) except where an explicit [] disabled a feature.
type Config struct {
	// SessionCommand is the argv each new session runs. `keen -- <cmd>`
	// still wins over this for the lifetime of that server.
	SessionCommand []string `json:"session_command"`

	// TasksCommand fills the sidebar's live-work section. It must print a
	// JSON array of {id, title, status, priority} objects on stdout (what
	// `bd list --json` emits). Empty ([]) hides the section.
	TasksCommand []string `json:"tasks_command"`

	// TasksLabel is the section header above the task rows. Defaults to the
	// non-flag words of TasksCommand ("bd list").
	TasksLabel string `json:"tasks_label"`

	// TitlerCommand produces the sidebar's topic/task/phase labels: the
	// titler appends its prompt as one final argument and reads stdout.
	// Empty ([]) disables LLM titling (a heuristic label is used instead).
	TitlerCommand []string `json:"titler_command"`
}

// Default is the zero-config behavior: Claude Code sessions, bd for tasks,
// Haiku (via the claude CLI) for titles. Fully resolved — safe to use as-is
// when Load fails.
func Default() Config {
	cmd := []string{"bd", "list", "--json"}
	return Config{
		SessionCommand: []string{"claude", "--permission-mode", "auto"},
		TasksCommand:   cmd,
		TasksLabel:     deriveLabel(cmd),
		TitlerCommand:  []string{"claude", "--model", "haiku", "-p"},
	}
}

// Load resolves the configuration for a keen instance launched from cwd:
// defaults, overlaid by the global file, overlaid by cwd's .keen.json. A
// missing file is fine; a file that exists but doesn't parse is an error (so
// typos surface at boot instead of silently reverting to defaults).
func Load(cwd string) (Config, error) {
	cfg := Config{}
	if err := overlay(&cfg, globalPath()); err != nil {
		return Config{}, err
	}
	if err := overlay(&cfg, filepath.Join(cwd, LocalFile)); err != nil {
		return Config{}, err
	}
	applyDefaults(&cfg)
	return cfg, nil
}

// overlay reads path (if it exists) and merges its fields into cfg: only
// fields present in the file are touched, so a local file can override one
// field while the global file supplies the rest.
func overlay(cfg *Config, path string) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var layer Config
	if err := json.Unmarshal(raw, &layer); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if layer.SessionCommand != nil {
		cfg.SessionCommand = layer.SessionCommand
	}
	if layer.TasksCommand != nil {
		cfg.TasksCommand = layer.TasksCommand
	}
	if layer.TasksLabel != "" {
		cfg.TasksLabel = layer.TasksLabel
	}
	if layer.TitlerCommand != nil {
		cfg.TitlerCommand = layer.TitlerCommand
	}
	return nil
}

// applyDefaults fills every still-unset field. nil means "no file mentioned
// it" (default applies); a non-nil empty slice means "explicitly disabled"
// and is left alone.
func applyDefaults(cfg *Config) {
	def := Default()
	if cfg.SessionCommand == nil {
		cfg.SessionCommand = def.SessionCommand
	}
	if cfg.TasksCommand == nil {
		cfg.TasksCommand = def.TasksCommand
	}
	if cfg.TitlerCommand == nil {
		cfg.TitlerCommand = def.TitlerCommand
	}
	if cfg.TasksLabel == "" {
		cfg.TasksLabel = deriveLabel(cfg.TasksCommand)
	}
}

// deriveLabel turns a tasks argv into a compact header: its non-flag words
// ("bd list --json" → "bd list").
func deriveLabel(argv []string) string {
	var words []string
	for _, a := range argv {
		if !strings.HasPrefix(a, "-") {
			words = append(words, a)
		}
	}
	return strings.Join(words, " ")
}

// globalPath is the user-wide config file location, honoring XDG_CONFIG_HOME.
func globalPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "keen", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "keen", "config.json")
}
