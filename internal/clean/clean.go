package clean

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"transom/internal/config"
	"transom/internal/proc"
	"transom/internal/scan"
)

// Modes.
const (
	ModeTrash  = "trash"
	ModeDelete = "delete"
)

// commandTimeout bounds one cleanup command (brew, simctl, docker).
const commandTimeout = 10 * time.Minute

// Request is a clean request (the /api/clean body).
type Request struct {
	Items  []string `json:"items"`
	Mode   string   `json:"mode"`
	DryRun bool     `json:"dryRun"`
}

// Failure is one item that couldn't be removed.
type Failure struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Result is the outcome (JSON per contract).
type Result struct {
	Freed   int64     `json:"freed"`
	Removed int       `json:"removed"`
	DryRun  bool      `json:"dryRun"`
	Failed  []Failure `json:"failed"`

	// RemovedIDs are the ids removed (or that would be, on a dry run),
	// so the caller can drop them from its stored scan.
	RemovedIDs []string `json:"-"`
}

// Run removes the requested items of res. Every id must belong to res —
// one unknown id fails the whole request before anything is touched.
// Paths come only from res, never from the request.
func Run(ctx context.Context, res *scan.Result, req Request) (*Result, error) {
	if res == nil {
		return nil, errors.New("no scan result: run a scan first")
	}
	mode := req.Mode
	if mode == "" {
		mode = ModeTrash
	}
	if mode != ModeTrash && mode != ModeDelete {
		return nil, fmt.Errorf("unknown mode %q (want %q or %q)", req.Mode, ModeTrash, ModeDelete)
	}
	if len(req.Items) == 0 {
		return nil, errors.New("no items selected")
	}
	var items []scan.Item
	seen := map[string]bool{}
	for _, id := range req.Items {
		if seen[id] {
			continue
		}
		seen[id] = true
		it, ok := res.Lookup(id)
		if !ok {
			return nil, fmt.Errorf("unknown item id %q (rescan and try again)", id)
		}
		items = append(items, it)
	}
	guard, err := DefaultGuard()
	if err != nil {
		return nil, err
	}

	out := &Result{DryRun: req.DryRun, Failed: []Failure{}}
	var cmds []scan.Item
	var files []scan.Item
	for _, it := range items {
		if it.Kind == scan.KindCommand {
			cmds = append(cmds, it)
		} else {
			files = append(files, it)
		}
	}
	// Parents before children: once a folder is gone, items inside it
	// (a duplicate inside an old download folder, say) are gone too and
	// their bytes were already counted with the folder.
	sort.SliceStable(files, func(i, j int) bool { return pathKey(files[i].Path) < pathKey(files[j].Path) })
	var done []string // paths removed so far (or that would be)
	// The per-category guard is computed once per run: on Windows it
	// re-lists and safety-checks the category's whole cleanup allowlist,
	// which is far too slow to repeat for every selected item.
	guards := map[string]Guard{}
	cats := []string{}
	catSeen := map[string]bool{}
	succeed := func(it scan.Item, freed int64) {
		out.Removed++
		out.Freed += freed
		out.RemovedIDs = append(out.RemovedIDs, it.ID)
		if !catSeen[it.Category] {
			catSeen[it.Category] = true
			cats = append(cats, it.Category)
		}
	}
	fail := func(it scan.Item, err error) {
		out.Failed = append(out.Failed, Failure{ID: it.ID, Path: it.Path, Error: err.Error()})
	}

	for _, it := range files {
		if ctx.Err() != nil {
			fail(it, ctx.Err())
			continue
		}
		covered := false
		for _, d := range done {
			if platformCovered(it.Path, d, req.DryRun) {
				covered = true
				break
			}
		}
		if covered {
			succeed(it, 0)
			continue
		}
		g, ok := guards[it.Category]
		if !ok {
			g = guard
			if it.Category == "stale-deps" {
				// Dependency folders may live on a projects volume outside
				// $HOME: allow exactly the roots this scan searched.
				g.ProjectRoots = res.Roots
			}
			g = platformGuard(g, it)
			guards[it.Category] = g
		}
		target, err := g.CheckContext(ctx, it.Path)
		if err != nil {
			fail(it, err)
			continue
		}
		if _, err := os.Lstat(target); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				err = errors.New("no longer exists")
			}
			fail(it, err)
			continue
		}
		if err := platformValidate(ctx, g.Home, target, it); err != nil {
			fail(it, err)
			continue
		}
		if !req.DryRun {
			// Trash items are already in the Trash: "cleaning" them
			// means emptying them for good.
			if permanentRemoval(mode, it) {
				err = os.RemoveAll(target) // removes symlinks, never follows them
			} else {
				err = moveToTrash(target, guard.Home)
			}
			if err != nil {
				fail(it, err)
				continue
			}
		}
		done = append(done, it.Path)
		succeed(it, it.Size)
	}

	for _, it := range cmds {
		if ctx.Err() != nil {
			fail(it, ctx.Err())
			continue
		}
		if handled, err := platformCommand(ctx, it, mode, req.DryRun); handled {
			if err != nil {
				fail(it, err)
			} else {
				succeed(it, it.Size)
			}
			continue
		}
		argv, ok := scan.Commands[it.Category]
		if !ok || len(argv) == 0 {
			fail(it, errors.New("no cleanup command for this category"))
			continue
		}
		bin := scan.FindTool(argv[0])
		if bin == "" {
			fail(it, fmt.Errorf("%s is not installed", argv[0]))
			continue
		}
		if !req.DryRun {
			if err := runCommand(ctx, bin, argv[1:]); err != nil {
				fail(it, err)
				continue
			}
		}
		succeed(it, it.Size)
	}

	if !req.DryRun && out.Removed > 0 {
		_ = config.AddHistory(config.HistoryEntry{
			At:         time.Now().UTC().Format(time.RFC3339),
			Freed:      out.Freed,
			Removed:    out.Removed,
			Mode:       mode,
			Categories: cats,
		})
	}
	return out, nil
}

// runCommand runs an allowlisted command (fixed argv, no shell).
func runCommand(ctx context.Context, bin string, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, bin, args...)
	proc.HideWindow(c)
	c.Env = scan.ToolEnv()
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = "…" + msg[len(msg)-300:]
		}
		if msg != "" {
			return fmt.Errorf("%s %s: %v: %s", filepath.Base(bin), strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %v", filepath.Base(bin), strings.Join(args, " "), err)
	}
	return nil
}

// trashName picks a free name in the trash for base.
func trashName(trash, base string, now time.Time) string {
	dest := filepath.Join(trash, base)
	if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
		return dest
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // dotfile like ".env": keep the whole name as stem
		stem, ext = base, ""
	}
	stamp := now.Format("2006-01-02 15.04.05")
	for i := 0; ; i++ {
		name := stem + " " + stamp
		if i > 0 {
			name += fmt.Sprintf(" %d", i+1)
		}
		dest = filepath.Join(trash, name+ext)
		if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
			return dest
		}
	}
}
