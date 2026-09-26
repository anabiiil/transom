package scan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	dupMinSize   = 1 << 20  // files under 1 MB aren't worth reporting
	partialChunk = 64 << 10 // bytes hashed from each end in the first pass
	hashWorkers  = 4
)

type dupFile struct {
	path string
	fi   fs.FileInfo
	st   statInfo
}

func scanDuplicates(ctx context.Context, e *Env) ([]Item, error) {
	roots := []string{
		filepath.Join(e.Home, "Desktop"),
		filepath.Join(e.Home, "Documents"),
		filepath.Join(e.Home, "Downloads"),
	}
	groups := findDuplicates(ctx, roots, e.sem, e.Prog)
	var items []Item
	for _, g := range groups {
		keep := g.files[0]
		for _, f := range g.files[1:] {
			items = append(items, Item{
				Path:    f.path,
				Label:   filepath.Base(f.path),
				Size:    f.st.alloc,
				ModTime: rfc3339(f.fi.ModTime()),
				Kind:    KindFile,
				Note:    "Duplicate of " + tildify(e.Home, keep.path),
				Group:   "dup-" + g.hash[:12],
			})
		}
	}
	return items, ctx.Err()
}

// dupGroup is a set of identical files, the one to keep first.
type dupGroup struct {
	hash  string
	files []dupFile
}

// findDuplicates finds identical regular files ≥ 1 MB under roots:
// same size → same hash of the first and last 64 KB → same full
// SHA-256. Hard links to one inode are one file, and cloud-only
// (dataless) files are skipped so nothing gets downloaded. In each
// group the newest file comes first.
func findDuplicates(ctx context.Context, roots []string, sem chan struct{}, prog *Progress) []dupGroup {
	var (
		mu     sync.Mutex
		bySize = map[int64][]dupFile{}
		inodes = map[[2]uint64]bool{}
	)
	walkTree(ctx, roots, walkMaxDepth, sem, prog, func(dir string, sib map[string]bool, name string, fi fs.FileInfo, depth int) bool {
		if fi.IsDir() {
			if _, dep := depKind(dir, sib, name); dep || name == "node_modules" {
				return false
			}
			return !strings.HasPrefix(name, ".") && !isBundle(name)
		}
		if !fi.Mode().IsRegular() || fi.Size() < dupMinSize {
			return false
		}
		st := statOf(fi)
		if st.dataless {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		if st.haveInode {
			k := [2]uint64{st.dev, st.ino}
			if inodes[k] {
				return false
			}
			inodes[k] = true
		}
		bySize[fi.Size()] = append(bySize[fi.Size()], dupFile{path: filepath.Join(dir, name), fi: fi, st: st})
		return false
	})

	var candidates [][]dupFile
	for _, list := range bySize {
		if len(list) > 1 {
			candidates = append(candidates, list)
		}
	}
	// Pass 1: partial hash within each size bucket; pass 2: full hash.
	var refined [][]dupFile
	for _, g := range regroup(ctx, candidates, partialHash) {
		refined = append(refined, g.files)
	}
	groups := regroup(ctx, refined, fullHash)
	for i := range groups {
		f := groups[i].files
		sort.Slice(f, func(a, b int) bool {
			ta, tb := f[a].fi.ModTime(), f[b].fi.ModTime()
			if !ta.Equal(tb) {
				return ta.After(tb)
			}
			if len(f[a].path) != len(f[b].path) {
				return len(f[a].path) < len(f[b].path)
			}
			return f[a].path < f[b].path
		})
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].hash < groups[b].hash })
	return groups
}

// regroup hashes every file of every bucket (concurrently) and splits
// the buckets by hash, keeping only sub-groups with 2+ files.
func regroup(ctx context.Context, buckets [][]dupFile, hash func(string, int64) (string, error)) []dupGroup {
	type job struct{ bucket, idx int }
	sums := make([][]string, len(buckets))
	for i, b := range buckets {
		sums[i] = make([]string, len(b))
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < hashWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					continue
				}
				f := buckets[j.bucket][j.idx]
				if h, err := hash(f.path, f.fi.Size()); err == nil {
					sums[j.bucket][j.idx] = h
				}
			}
		}()
	}
	for bi, b := range buckets {
		for i := range b {
			jobs <- job{bi, i}
		}
	}
	close(jobs)
	wg.Wait()

	var out []dupGroup
	for bi, b := range buckets {
		byHash := map[string][]dupFile{}
		for i, f := range b {
			if h := sums[bi][i]; h != "" {
				byHash[h] = append(byHash[h], f)
			}
		}
		for h, list := range byHash {
			if len(list) > 1 {
				out = append(out, dupGroup{hash: h, files: list})
			}
		}
	}
	return out
}

// partialHash hashes the size plus the first and last 64 KB.
func partialHash(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, partialChunk)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", err
	}
	h.Write(buf[:n])
	if size > 2*partialChunk {
		if _, err := f.Seek(size-partialChunk, io.SeekStart); err != nil {
			return "", err
		}
		n, err = io.ReadFull(f, buf)
		if err != nil && err != io.ErrUnexpectedEOF {
			return "", err
		}
		h.Write(buf[:n])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fullHash is the SHA-256 of the whole file.
func fullHash(path string, _ int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
