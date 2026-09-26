// Package scan finds reclaimable disk space. Everything here is
// strictly read-only: scanners stat, list and (for duplicates) read
// files, and shell out only to read-only tool subcommands. Removing
// anything is the clean package's job.
package scan

import (
	"crypto/sha1"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// Risk levels (see docs/CONTRACT.md, safety principle 5).
const (
	RiskSafe    = "safe"
	RiskReview  = "review"
	RiskCaution = "caution"
)

// Item kinds.
const (
	KindDir     = "dir"
	KindFile    = "file"
	KindCommand = "command"
)

// Category describes one kind of reclaimable space.
type Category struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
	Icon        string `json:"icon"`
}

// Options tune the scanners that need thresholds or roots.
type Options struct {
	StaleDays  int      `json:"staleDays"`
	LargeMinMB int      `json:"largeMinMB"`
	Roots      []string `json:"roots"`
}

// withDefaults fills zero values: 60 stale days, 500 MB, roots = [home].
func (o Options) withDefaults(home string) Options {
	if o.StaleDays <= 0 {
		o.StaleDays = 60
	}
	if o.LargeMinMB <= 0 {
		o.LargeMinMB = 500
	}
	var roots []string
	for _, r := range o.Roots {
		if r != "" {
			roots = append(roots, r)
		}
	}
	if len(roots) == 0 {
		roots = []string{home}
	}
	o.Roots = roots
	return o
}

// Item is one removable thing: a file, a directory or a command.
type Item struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Label    string `json:"label"`
	Size     int64  `json:"size"`
	ModTime  string `json:"modTime"`
	Kind     string `json:"kind"`
	Risk     string `json:"risk"`
	Note     string `json:"note,omitempty"`
	Group    string `json:"group,omitempty"`
	Category string `json:"-"` // owning category id (set by Run)
}

// CategoryResult is one category's findings.
type CategoryResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Risk        string `json:"risk"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	TotalSize   int64  `json:"totalSize"`
	Count       int    `json:"count"`
	Items       []Item `json:"items"`
}

// Result is a complete scan.
type Result struct {
	ScannedAt  string           `json:"scannedAt"`
	TotalSize  int64            `json:"totalSize"`
	Categories []CategoryResult `json:"categories"`

	// Roots are the project roots stale-deps searched. Clean lets the
	// guard accept dependency folders under them (and nothing else).
	Roots []string `json:"-"`
}

// Lookup returns the item with the given id, if any.
func (r *Result) Lookup(id string) (Item, bool) {
	if r == nil {
		return Item{}, false
	}
	for _, c := range r.Categories {
		for _, it := range c.Items {
			if it.ID == id {
				it.Category = c.ID
				return it, true
			}
		}
	}
	return Item{}, false
}

// HasPath reports whether a non-command item of the scan has exactly path p.
func (r *Result) HasPath(p string) bool {
	if r == nil {
		return false
	}
	for _, c := range r.Categories {
		for _, it := range c.Items {
			if it.Kind != KindCommand && it.Path == p {
				return true
			}
		}
	}
	return false
}

// Without returns a copy of the result minus the given item ids, with
// totals recomputed (the server swaps it in after a clean so the stored
// scan stays truthful). The receiver is not modified.
func (r *Result) Without(ids []string) *Result {
	if r == nil {
		return nil
	}
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}
	out := &Result{ScannedAt: r.ScannedAt, Roots: r.Roots, Categories: make([]CategoryResult, len(r.Categories))}
	for i, c := range r.Categories {
		c.Items = append([]Item{}, c.Items...)
		kept := c.Items[:0]
		for _, it := range c.Items {
			if !drop[it.ID] {
				kept = append(kept, it)
			}
		}
		c.Items = kept
		out.Categories[i] = c
	}
	out.recompute()
	return out
}

// recompute refreshes per-category and overall totals. The overall total
// counts each path once: categories can overlap (a stale app's cache is
// both a user cache and a leftover; a large file can be a duplicate).
func (r *Result) recompute() {
	seen := map[string]bool{}
	r.TotalSize = 0
	for i := range r.Categories {
		c := &r.Categories[i]
		c.TotalSize, c.Count = 0, len(c.Items)
		for _, it := range c.Items {
			c.TotalSize += it.Size
			key := it.Path
			if it.Kind == KindCommand {
				key = "cmd:" + c.ID + ":" + it.Path
			}
			if !seen[key] {
				seen[key] = true
				r.TotalSize += it.Size
			}
		}
	}
}

// ItemID is the stable id of an item: short sha1 of category + path.
func ItemID(category, path string) string {
	h := sha1.Sum([]byte(category + "\x00" + path))
	return hex.EncodeToString(h[:6])
}

// Progress is updated by a running scan and read concurrently by the UI.
type Progress struct {
	mu       sync.Mutex
	category string
	started  time.Time
	scanned  atomic.Int64
	found    atomic.Int64
	elapsed  atomic.Int64 // frozen elapsed ms once the scan ended (0 = running)
}

// NewProgress starts the clock.
func NewProgress() *Progress { return &Progress{started: time.Now()} }

func (p *Progress) setCategory(id string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.category = id
	p.mu.Unlock()
}

// finish freezes the elapsed time.
func (p *Progress) finish() {
	if p != nil {
		p.elapsed.Store(max(time.Since(p.started).Milliseconds(), 1))
	}
}

// AddScanned counts filesystem entries visited.
func (p *Progress) AddScanned(n int64) {
	if p != nil {
		p.scanned.Add(n)
	}
}

// AddFound counts items found.
func (p *Progress) AddFound(n int64) {
	if p != nil {
		p.found.Add(n)
	}
}

// Snapshot is a point-in-time copy of Progress (JSON shape per contract).
type Snapshot struct {
	Category  string `json:"category"`
	Scanned   int64  `json:"scanned"`
	Found     int64  `json:"found"`
	ElapsedMs int64  `json:"elapsedMs"`
}

// Snapshot reads the counters.
func (p *Progress) Snapshot() Snapshot {
	if p == nil {
		return Snapshot{}
	}
	p.mu.Lock()
	cat := p.category
	p.mu.Unlock()
	ms := p.elapsed.Load()
	if ms == 0 {
		ms = time.Since(p.started).Milliseconds()
	}
	return Snapshot{
		Category:  cat,
		Scanned:   p.scanned.Load(),
		Found:     p.found.Load(),
		ElapsedMs: ms,
	}
}
