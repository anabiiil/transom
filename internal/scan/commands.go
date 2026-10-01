package scan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"transom/internal/proc"
)

// Commands is the allowlist of cleanup commands, keyed by category id.
// A command item is executed only by looking its category up here — the
// item's own path/label is display text and is never executed.
var Commands = platformCommands(runtime.GOOS)

func platformCommands(goos string) map[string][]string {
	commands := map[string][]string{"docker": {"docker", "system", "prune", "-f"}}
	if goos != "windows" {
		commands["simulators"] = []string{"xcrun", "simctl", "delete", "unavailable"}
		commands["homebrew"] = []string{"brew", "cleanup", "-s"}
	}
	return commands
}

// toolDirs are searched after $PATH: a GUI app (Transom.app) inherits
// launchd's minimal PATH, which lacks Homebrew and Docker.
var toolDirs = []string{
	"/opt/homebrew/bin",
	"/usr/local/bin",
	"/Applications/Docker.app/Contents/Resources/bin",
	"/usr/bin",
}

// FindTool resolves a tool's absolute path, or "" if it isn't installed.
func FindTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	if runtime.GOOS == "windows" {
		if name == "docker" {
			programFiles := os.Getenv("ProgramFiles")
			if programFiles != "" && filepath.IsAbs(programFiles) {
				p := filepath.Join(programFiles, "Docker", "Docker", "resources", "bin", "docker.exe")
				if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
					return p
				}
			}
		}
		return ""
	}
	for _, d := range toolDirs {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// ToolEnv is the environment tool commands run with: quiet Homebrew
// (no auto-update, no analytics, no hints).
func ToolEnv() []string {
	if runtime.GOOS == "windows" {
		return os.Environ()
	}
	return append(os.Environ(),
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"HOMEBREW_NO_ANALYTICS=1",
		"HOMEBREW_NO_ENV_HINTS=1",
		"HOMEBREW_NO_INSTALL_CLEANUP=1",
	)
}

// output runs a read-only tool subcommand with a timeout.
func output(ctx context.Context, timeout time.Duration, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := exec.CommandContext(ctx, bin, args...)
	proc.HideWindow(c)
	c.Env = ToolEnv()
	c.Stdin = nil
	return c.Output()
}

func commandItem(category, label, note string, size int64) Item {
	return Item{
		Path:  strings.Join(Commands[category], " "),
		Label: label,
		Size:  size,
		Kind:  KindCommand,
		Note:  note,
	}
}

// --- simulators ---

func scanSimulators(ctx context.Context, e *Env) ([]Item, error) {
	// simctl ships with Xcode, not the Command Line Tools; asking the
	// /usr/bin/xcrun shim for it without Xcode can pop an install
	// dialog, so check the active developer dir first.
	xs := FindTool("xcode-select")
	if xs == "" {
		return nil, nil
	}
	dir, err := output(ctx, 5*time.Second, xs, "-p")
	if err != nil || !strings.Contains(string(dir), ".app/") {
		return nil, nil
	}
	xcrun := FindTool("xcrun")
	if xcrun == "" {
		return nil, nil
	}
	out, err := output(ctx, 30*time.Second, xcrun, "simctl", "list", "devices", "unavailable", "-j")
	if err != nil {
		return nil, err
	}
	udids, err := parseUnavailableDevices(out)
	if err != nil || len(udids) == 0 {
		return nil, err
	}
	devices := filepath.Join(e.Home, "Library", "Developer", "CoreSimulator", "Devices")
	var paths []string
	for _, u := range udids {
		paths = append(paths, filepath.Join(devices, u))
	}
	var total int64
	for _, s := range e.sizePaths(ctx, paths) {
		total += s.size
	}
	label := strconv.Itoa(len(udids)) + " unavailable simulator"
	if len(udids) != 1 {
		label += "s"
	}
	return []Item{commandItem("simulators", label, "Runs: xcrun simctl delete unavailable", total)}, nil
}

// parseUnavailableDevices reads `simctl list devices unavailable -j`.
func parseUnavailableDevices(b []byte) ([]string, error) {
	var v struct {
		Devices map[string][]struct {
			UDID string `json:"udid"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	var out []string
	for _, list := range v.Devices {
		for _, d := range list {
			if d.UDID != "" && !strings.ContainsAny(d.UDID, "/.") {
				out = append(out, d.UDID)
			}
		}
	}
	return out, nil
}

// --- homebrew ---

func scanHomebrew(ctx context.Context, e *Env) ([]Item, error) {
	brew := FindTool("brew")
	if brew == "" {
		return nil, nil
	}
	cache := os.Getenv("HOMEBREW_CACHE")
	if cache == "" {
		cache = filepath.Join(e.Home, "Library", "Caches", "Homebrew")
	}
	var items []Item
	if fi, err := os.Lstat(cache); err == nil && fi.IsDir() {
		for _, s := range e.sizePaths(ctx, []string{cache}) {
			items = append(items, Item{
				Path: s.path, Label: "Download cache", Size: s.size,
				ModTime: rfc3339(s.newest), Kind: KindDir,
				Note: "Re-downloaded when needed",
			})
		}
	}
	// What `brew cleanup -s` frees beyond the cache (old versions in
	// the Cellar, stale locks...). Cache paths are already an item.
	if out, err := output(ctx, 90*time.Second, brew, "cleanup", "-n", "-s"); err == nil {
		if n := brewReclaim(out, cache); n > 0 {
			items = append(items, commandItem("homebrew", "Old versions (brew cleanup -s)", "Runs: brew cleanup -s", n))
		}
	}
	return items, nil
}

var brewLine = regexp.MustCompile(`^Would remove: (.+) \((?:[\d,]+ files?, )?([\d.]+)(B|KB|MB|GB|TB)\)$`)

// brewReclaim sums `brew cleanup -n` lines outside cacheDir. Homebrew
// prints sizes in base 1024.
func brewReclaim(out []byte, cacheDir string) int64 {
	var total int64
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		m := brewLine.FindStringSubmatch(strings.TrimSpace(sc.Text()))
		if m == nil {
			continue
		}
		if p := m[1]; p == cacheDir || strings.HasPrefix(p, cacheDir+"/") {
			continue
		}
		f, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		mult := map[string]float64{"B": 1, "KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30, "TB": 1 << 40}[m[3]]
		total += int64(f * mult)
	}
	return total
}

// --- docker ---

func scanDocker(ctx context.Context, e *Env) ([]Item, error) {
	docker := FindTool("docker")
	if docker == "" {
		return nil, nil
	}
	out, err := output(ctx, 20*time.Second, docker, "system", "df", "--format", "{{json .}}")
	if err != nil {
		return nil, nil // daemon not running: nothing prune could do now
	}
	n := dockerReclaim(out)
	if n <= 0 {
		return nil, nil
	}
	return []Item{commandItem("docker", "Unused Docker data (docker system prune -f)",
		"Estimate: stopped containers + build cache. Runs: docker system prune -f", n)}, nil
}

// dockerReclaim sums the reclaimable space prune actually targets:
// containers and build cache (it removes only dangling images, which df
// doesn't break out, so images are left out of the estimate).
func dockerReclaim(out []byte) int64 {
	var total int64
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		var row struct {
			Type        string `json:"Type"`
			Reclaimable string `json:"Reclaimable"`
		}
		if json.Unmarshal(sc.Bytes(), &row) != nil {
			continue
		}
		if row.Type != "Containers" && row.Type != "Build Cache" {
			continue
		}
		total += parseDockerSize(strings.Fields(row.Reclaimable + " ")[0])
	}
	return total
}

var dockerSize = regexp.MustCompile(`^([\d.]+)\s*([kKMGT]?B)$`)

// parseDockerSize parses docker's base-1000 sizes ("1.2GB", "512kB", "0B").
func parseDockerSize(s string) int64 {
	m := dockerSize.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}[m[2]]
	return int64(f * mult)
}
