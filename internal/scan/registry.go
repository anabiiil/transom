package scan

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"transom/internal/config"
)

// Env is what every scanner gets: the home directory it works under,
// the options, the shared progress counters and the shared parallelism
// budget.
type Env struct {
	Home string
	Opts Options
	Prog *Progress

	sem chan struct{}

	idxMu sync.Mutex
	idx   map[string]*treeIndex
}

type scannerFunc func(ctx context.Context, e *Env) ([]Item, error)

type def struct {
	Category
	scan scannerFunc
}

// registry lists every category in display order. IDs are the contract's.
var registry = []def{
	{Category{"user-caches", "User caches", "system", "App caches in ~/Library/Caches. Apps rebuild them as needed.", RiskSafe, "cache"}, scanUserCaches},
	{Category{"user-logs", "Logs", "system", "App logs and diagnostic reports in ~/Library/Logs.", RiskSafe, "log"}, scanUserLogs},
	{Category{"temp-files", "Temporary files", "system", "Your temporary folder's entries untouched for over a day.", RiskSafe, "file"}, scanTempFiles},
	{Category{"trash", "Trash", "system", "Items already in the Trash. Cleaning empties them permanently.", RiskReview, "trash"}, scanTrash},
	{Category{"mail-downloads", "Mail downloads", "system", "Attachments Mail saved when you opened them.", RiskSafe, "file"}, scanMailDownloads},
	{Category{"xcode", "Xcode", "developer", "DerivedData, device support files, simulator caches and archives.", RiskSafe, "xcode"}, scanXcode},
	{Category{"simulators", "Unavailable simulators", "developer", "Simulators for runtimes no longer installed (xcrun simctl delete unavailable).", RiskSafe, "xcode"}, scanSimulators},
	{Category{"package-caches", "Package manager caches", "developer", "Download caches of npm, Yarn, pnpm, Composer, pip, Go, Gradle, CocoaPods and Bun.", RiskSafe, "box"}, scanPackageCaches},
	{Category{"homebrew", "Homebrew", "developer", "Homebrew's download cache and old versions (brew cleanup -s).", RiskSafe, "box"}, scanHomebrew},
	{Category{"docker", "Docker", "developer", "Stopped containers, unused networks, dangling images and build cache (docker system prune -f).", RiskReview, "docker"}, scanDocker},
	{Category{"stale-deps", "Stale dependencies", "projects", "node_modules, vendor, .venv and target folders in projects you haven't touched in a while.", RiskReview, "node"}, scanStaleDeps},
	{Category{"large-files", "Large files", "files", "Big files in your home folder.", RiskCaution, "file"}, scanLargeFiles},
	{Category{"old-downloads", "Old downloads", "files", "Items in Downloads older than 90 days.", RiskReview, "file"}, scanOldDownloads},
	{Category{"duplicates", "Duplicates", "files", "Identical copies in Desktop, Documents and Downloads. The newest copy is kept.", RiskCaution, "copy"}, scanDuplicates},
	{Category{"app-leftovers", "App leftovers", "files", "Support files of apps that are no longer installed.", RiskReview, "app"}, scanAppLeftovers},
}

// Categories returns every category in display order.
func Categories() []Category {
	out := make([]Category, len(registry))
	for i, d := range registry {
		out[i] = d.Category
	}
	return out
}

// LookupCategory returns the category with id.
func LookupCategory(id string) (Category, bool) {
	for _, d := range registry {
		if d.ID == id {
			return d.Category, true
		}
	}
	return Category{}, false
}

// Run scans the given categories (all when ids is empty) under the
// current user's home directory. It returns ctx.Err() if cancelled. A
// scanner that fails (a tool misbehaving, say) contributes no items
// rather than failing the whole scan.
func Run(ctx context.Context, ids []string, opts Options, prog *Progress) (*Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var defs []def
	if len(ids) == 0 {
		defs = registry
	} else {
		want := map[string]bool{}
		for _, id := range ids {
			if _, ok := LookupCategory(id); !ok {
				return nil, fmt.Errorf("unknown category %q", id)
			}
			want[id] = true
		}
		for _, d := range registry {
			if want[d.ID] {
				defs = append(defs, d)
			}
		}
	}
	if prog == nil {
		prog = NewProgress()
	}
	defer prog.finish()
	if len(opts.Roots) == 0 {
		// The projectRoots preference (default: home); --root / the
		// API's options.roots override it.
		if roots, err := config.ProjectRoots(); err == nil {
			opts.Roots = roots
		}
	}
	env := &Env{
		Home: home,
		Opts: opts.withDefaults(home),
		Prog: prog,
		sem:  make(chan struct{}, parallelism),
	}

	res := &Result{ScannedAt: time.Now().UTC().Format(time.RFC3339), Categories: []CategoryResult{}, Roots: env.Opts.Roots}
	for _, d := range defs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prog.setCategory(d.ID)
		items, _ := d.scan(ctx, env)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res.Categories = append(res.Categories, finish(d.Category, items))
		prog.AddFound(int64(len(items)))
	}
	res.recompute()
	return res, nil
}

// finish stamps ids/risk on a category's items, drops duplicates and
// empties, and sorts biggest first.
func finish(c Category, items []Item) CategoryResult {
	cr := CategoryResult{ID: c.ID, Name: c.Name, Group: c.Group, Risk: c.Risk,
		Description: c.Description, Icon: c.Icon, Items: []Item{}}
	seen := map[string]bool{}
	for _, it := range items {
		if it.Kind != KindCommand && it.Size <= 0 {
			continue
		}
		it.ID = ItemID(c.ID, it.Path)
		if seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		if it.Risk == "" {
			it.Risk = c.Risk
		}
		if it.Label == "" {
			it.Label = it.Path
		}
		it.Category = c.ID
		cr.Items = append(cr.Items, it)
	}
	sort.SliceStable(cr.Items, func(i, j int) bool { return cr.Items[i].Size > cr.Items[j].Size })
	return cr
}
