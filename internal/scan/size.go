package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// parallelism bounds concurrent directory reads across a whole scan.
const parallelism = 8

// sizeAcc accumulates one tree's totals from several goroutines.
type sizeAcc struct {
	size   atomic.Int64
	mu     sync.Mutex
	newest time.Time
	links  sync.Map // "dev:ino" of multiply-linked files already counted
}

func (a *sizeAcc) touch(t time.Time) {
	a.mu.Lock()
	if t.After(a.newest) {
		a.newest = t
	}
	a.mu.Unlock()
}

// sizeTree returns the allocated size (st_blocks*512, like Finder's "on
// disk") of path and the newest mtime found anywhere inside it. It never
// follows symlinks (a link counts as the link itself), counts each
// hard-linked inode once, and silently skips what it can't read (EPERM
// from TCC-protected folders is normal). sem bounds the extra
// goroutines; when it's full the walk continues inline, so it can't
// deadlock however deep the tree is.
func sizeTree(ctx context.Context, path string, sem chan struct{}, prog *Progress) (int64, time.Time) {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0, time.Time{}
	}
	prog.AddScanned(1)
	st := statOf(fi)
	if !fi.IsDir() {
		return st.alloc, fi.ModTime()
	}
	acc := &sizeAcc{newest: fi.ModTime()}
	acc.size.Add(st.alloc)
	var wg sync.WaitGroup
	walkSize(ctx, path, acc, sem, prog, &wg)
	wg.Wait()
	return acc.size.Load(), acc.newest
}

func walkSize(ctx context.Context, dir string, acc *sizeAcc, sem chan struct{}, prog *Progress, wg *sync.WaitGroup) {
	if ctx.Err() != nil {
		return
	}
	entries, err := readDirUnsorted(dir)
	if err != nil {
		return
	}
	prog.AddScanned(int64(len(entries)))
	for _, e := range entries {
		info, err := e.Info() // lstat semantics: symlinks are not followed
		if err != nil {
			continue
		}
		st := statOf(info)
		if !info.IsDir() && st.haveInode && st.nlink > 1 {
			key := [2]uint64{st.dev, st.ino}
			if _, dup := acc.links.LoadOrStore(key, struct{}{}); dup {
				continue
			}
		}
		acc.size.Add(st.alloc)
		acc.touch(info.ModTime())
		if !info.IsDir() {
			continue
		}
		child := filepath.Join(dir, e.Name())
		select {
		case sem <- struct{}{}:
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				walkSize(ctx, child, acc, sem, prog, wg)
			}()
		default:
			walkSize(ctx, child, acc, sem, prog, wg)
		}
	}
}

// readDirUnsorted lists dir without the sort os.ReadDir does.
func readDirUnsorted(dir string) ([]fs.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// sized is one path with its measured totals.
type sized struct {
	path   string
	info   fs.FileInfo
	size   int64
	newest time.Time // newest mtime inside (== mtime for files)
}

// sizePaths measures several paths concurrently (bounded), skipping any
// that vanished or can't be stat'ed. Order is not preserved.
func (e *Env) sizePaths(ctx context.Context, paths []string) []sized {
	var (
		mu  sync.Mutex
		out []sized
		wg  sync.WaitGroup
	)
	work := make(chan string)
	workers := parallelism
	if len(paths) < workers {
		workers = len(paths)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range work {
				if ctx.Err() != nil {
					continue
				}
				fi, err := os.Lstat(p)
				if err != nil {
					continue
				}
				size, newest := sizeTree(ctx, p, e.sem, e.Prog)
				mu.Lock()
				out = append(out, sized{path: p, info: fi, size: size, newest: newest})
				mu.Unlock()
			}
		}()
	}
	for _, p := range paths {
		work <- p
	}
	close(work)
	wg.Wait()
	return out
}

// junkNames are Finder droppings never worth an item of their own.
var junkNames = map[string]bool{".DS_Store": true, ".localized": true}

// children lists dir's entries as absolute paths (nil if unreadable),
// dropping Finder droppings.
func children(dir string) []string {
	entries, err := readDirUnsorted(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if junkNames[e.Name()] {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

func kindOf(fi fs.FileInfo) string {
	if fi.IsDir() {
		return KindDir
	}
	return KindFile
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
