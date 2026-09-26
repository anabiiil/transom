// Package config persists Transom's small amount of state under
// ~/.transom: UI preferences (config.json) and the clean history
// (history.json).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MaxHistory is how many clean runs history.json keeps.
const MaxHistory = 50

// mu serializes read-modify-write cycles within this process.
var mu sync.Mutex

// Dir is ~/.transom (resolved from $HOME at call time).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".transom"), nil
}

// Config is config.json.
type Config struct {
	Prefs map[string]string `json:"prefs"`
}

// HistoryEntry is one clean run (JSON per contract).
type HistoryEntry struct {
	At         string   `json:"at"`
	Freed      int64    `json:"freed"`
	Removed    int      `json:"removed"`
	Mode       string   `json:"mode"`
	Categories []string `json:"categories"`
}

func path(name string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name), nil
}

// readJSON decodes file into v; a missing file leaves v untouched.
func readJSON(name string, v any) error {
	p, err := path(name)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON atomically replaces file with v (0600, dir 0700).
func writeJSON(name string, v any) error {
	p, err := path(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+name+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Load reads config.json (zero value if absent).
func Load() (Config, error) {
	mu.Lock()
	defer mu.Unlock()
	return load()
}

func load() (Config, error) {
	var c Config
	err := readJSON("config.json", &c)
	if c.Prefs == nil {
		c.Prefs = map[string]string{}
	}
	return c, err
}

// Prefs returns all preferences.
func Prefs() (map[string]string, error) {
	c, err := Load()
	return c.Prefs, err
}

// PrefProjectRoots is the preference holding the folders stale-deps
// searches, as a JSON-encoded list of absolute paths ("~/" allowed),
// e.g. `["~", "/Volumes/Work/Projects"]`. Unset or empty = [$HOME].
const PrefProjectRoots = "projectRoots"

// ParseRoots decodes a projectRoots value: a JSON list of absolute paths
// (or "~", "~/..."), returned cleaned and with ~ expanded.
func ParseRoots(value string) ([]string, error) {
	var raw []string
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil, fmt.Errorf("%s must be a JSON list of paths: %v", PrefProjectRoots, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range raw {
		r = strings.TrimSpace(r)
		switch {
		case r == "":
			continue
		case r == "~":
			r = home
		case strings.HasPrefix(r, "~/"):
			r = filepath.Join(home, r[2:])
		}
		if !filepath.IsAbs(r) {
			return nil, fmt.Errorf("%s: not an absolute path: %s", PrefProjectRoots, r)
		}
		out = append(out, filepath.Clean(r))
	}
	return out, nil
}

// ProjectRoots returns the configured project roots, or [$HOME].
func ProjectRoots() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	c, err := Load()
	if err != nil {
		return []string{home}, err
	}
	if v := strings.TrimSpace(c.Prefs[PrefProjectRoots]); v != "" {
		roots, err := ParseRoots(v)
		if err != nil {
			return []string{home}, err
		}
		if len(roots) > 0 {
			return roots, nil
		}
	}
	return []string{home}, nil
}

// SetPref stores one preference. projectRoots is validated first.
func SetPref(key, value string) error {
	if key == "" {
		return errors.New("empty preference key")
	}
	if key == PrefProjectRoots && strings.TrimSpace(value) != "" {
		if _, err := ParseRoots(value); err != nil {
			return err
		}
	}
	mu.Lock()
	defer mu.Unlock()
	c, err := load()
	if err != nil {
		return err
	}
	c.Prefs[key] = value
	return writeJSON("config.json", c)
}

// History returns past clean runs, newest first.
func History() ([]HistoryEntry, error) {
	mu.Lock()
	defer mu.Unlock()
	return history()
}

func history() ([]HistoryEntry, error) {
	h := []HistoryEntry{}
	if err := readJSON("history.json", &h); err != nil {
		return []HistoryEntry{}, err
	}
	if h == nil {
		h = []HistoryEntry{}
	}
	return h, nil
}

// AddHistory prepends e, keeping at most MaxHistory entries. A corrupt
// history file is replaced rather than blocking the record.
func AddHistory(e HistoryEntry) error {
	mu.Lock()
	defer mu.Unlock()
	h, _ := history()
	h = append([]HistoryEntry{e}, h...)
	if len(h) > MaxHistory {
		h = h[:MaxHistory]
	}
	return writeJSON("history.json", h)
}
