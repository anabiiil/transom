//go:build windows

package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"transom/internal/proc"
)

func TestWindowsJunctionCannotEscapeScanRoots(t *testing.T) {
	root := realTemp(t)
	outside := realTemp(t)
	write(t, filepath.Join(outside, "private.bin"), randomBytes(t, 2<<20))
	junction := filepath.Join(root, "linked")
	command := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, outside)
	proc.HideWindow(command)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
	fi, err := os.Lstat(junction)
	if err != nil || !isReparse(fi) || WindowsPathSafe(junction) || WindowsPathSafe(filepath.Join(junction, "private.bin")) {
		t.Fatalf("junction or linked child accepted: %+v %v", fi, err)
	}
	visited := 0
	walkTree(context.Background(), []string{root}, 4, make(chan struct{}, 1), nil, func(_ string, _ map[string]bool, _ string, _ os.FileInfo, _ int) bool {
		visited++
		return true
	})
	if visited != 0 {
		t.Fatalf("junction visited or followed: %d", visited)
	}
	if size, _ := sizeTree(context.Background(), root, make(chan struct{}, 1), nil); size >= 2<<20 {
		t.Fatalf("junction target counted: %d", size)
	}
	if _, err := fullHash(filepath.Join(junction, "private.bin"), 2<<20); err == nil {
		t.Fatal("hash reader followed a junction")
	}
}

func TestWindowsPathSafeMissingLeafAndParent(t *testing.T) {
	root := realTemp(t)
	if !WindowsPathSafe(filepath.Join(root, "gone.bin")) {
		t.Fatal("missing final leaf should remain reportable")
	}
	if WindowsPathSafe(filepath.Join(root, "missing-parent", "gone.bin")) {
		t.Fatal("missing intermediate parent accepted")
	}
	if WindowsPathSafe("relative.bin") {
		t.Fatal("relative path accepted")
	}
	for _, path := range []string{root + ":stream", `\\?\C:\Users\file.bin`, `\\.\C:\Users\file.bin`} {
		if WindowsPathSafe(path) {
			t.Errorf("nonlocal or special path accepted: %s", path)
		}
	}
}

func TestWindowsFileIdentityAndAllocatedSize(t *testing.T) {
	root := realTemp(t)
	original := filepath.Join(root, "data.bin")
	copy := filepath.Join(root, "link.bin")
	write(t, original, randomBytes(t, 12345))
	if err := os.Link(original, copy); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	first, _ := os.Lstat(original)
	second, _ := os.Lstat(copy)
	a, b := statPath(original, first), statPath(copy, second)
	if !a.haveInode || !b.haveInode || a.ino != b.ino || a.dev != b.dev || a.nlink != 2 {
		t.Fatalf("hard-link metadata missing: %+v / %+v", a, b)
	}
	if a.alloc < first.Size() || a.birth.IsZero() {
		t.Fatalf("allocated size or creation time unavailable: %+v", a)
	}
	if size, _ := sizeTree(context.Background(), root, make(chan struct{}, 1), nil); size != a.alloc {
		t.Fatalf("hard-linked bytes counted twice: %d, want %d", size, a.alloc)
	}
}
