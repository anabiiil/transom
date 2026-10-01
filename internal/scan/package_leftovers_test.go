package scan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageFamilyRecognizesOnlyWindowsPackageIdentity(t *testing.T) {
	for _, family := range []string{
		"Microsoft.Windows.Photos_8wekyb3d8bbwe",
		"Acme.Store-App_abcdefghjkmnp",
		"ABC_ABCDEFGHJKMNP",
		strings.Repeat("a", 50) + "_8wekyb3d8bbwe",
	} {
		if !validPackageFamily(family) {
			t.Errorf("valid package family refused: %q", family)
		}
	}
	for _, family := range []string{
		"", "Google", "Packages", "Acme_abcdefghijklmn",
		"Acme_8wekyb3d8bbw", "Acme_8wekyb3d8bbwee",
		"Acme_abcdefghijklm", // I and L do not occur in publisher IDs
		"A_8wekyb3d8bbwe", strings.Repeat("a", 51) + "_8wekyb3d8bbwe",
		"Acme_App_8wekyb3d8bbwe", "Acme/_8wekyb3d8bbwe",
		"Acme\\_8wekyb3d8bbwe", ".._8wekyb3d8bbwe",
		"Acme._8wekyb3d8bbwe", "CON_8wekyb3d8bbwe",
		"com1.App_8wekyb3d8bbwe", "LPT9_8wekyb3d8bbwe",
		"xn--Acme_8wekyb3d8bbwe", "Acme.xn--Other_8wekyb3d8bbwe",
	} {
		if validPackageFamily(family) {
			t.Errorf("invalid or generic app name accepted: %q", family)
		}
	}
}

func TestPackageLeftoversKeepInstalledUnknownGenericAndEmptyData(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", local)
	root := filepath.Join(local, "Packages")
	orphan := "Acme.Removed_abcdefghjkmnp"
	installed := "Acme.Installed_abcdefghjkmnp"
	unknown := "Acme.Unknown_abcdefghjkmnp"
	bufferError := "Acme.BufferError_abcdefghjkmnp"
	for _, family := range []string{orphan, installed, unknown, bufferError, "Google", "Packages", "Acme_BadIdentity"} {
		write(t, filepath.Join(root, family, "LocalState", "settings.dat"), []byte("saved user data"))
	}
	for _, family := range []string{"Acme.Empty_abcdefghjkmnp", "Acme.EmptyTree_abcdefghjkmnp"} {
		if err := os.MkdirAll(filepath.Join(root, family, "LocalState", "empty"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(root, "Acme.Zero_abcdefghjkmnp", "LocalState", "zero.dat"), nil)
	queried := map[string]bool{}
	query := func(family string) (uint32, error) {
		queried[family] = true
		switch family {
		case installed:
			return 1, nil
		case unknown:
			return 0, errors.New("registration unavailable")
		case bufferError:
			return 3, errors.New("insufficient buffer")
		default:
			return 0, nil
		}
	}
	paths := windowsPackageLeftovers(home, query)
	if len(paths) != 1 || paths[0] != filepath.Join(root, orphan) {
		t.Fatalf("unsafe leftover selection: %v", paths)
	}
	for _, generic := range []string{"Google", "Packages", "Acme_BadIdentity"} {
		if queried[generic] {
			t.Errorf("generic directory queried as package identity: %s", generic)
		}
	}
	if paths := windowsPackageLeftovers(home, nil); len(paths) != 0 {
		t.Fatalf("missing registration query removed data: %v", paths)
	}
}

func TestPackageLeftoversRecheckRegistrationAfterReinstall(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", local)
	family := "Acme.Reinstalled_abcdefghjkmnp"
	path := filepath.Join(local, "Packages", family)
	write(t, filepath.Join(path, "LocalState", "settings.dat"), []byte("saved data"))
	count := uint32(0)
	query := func(string) (uint32, error) { return count, nil }
	if paths := windowsPackageLeftovers(home, query); len(paths) != 1 || paths[0] != path {
		t.Fatalf("removed package not found: %v", paths)
	}
	count = 1
	if paths := windowsPackageLeftovers(home, query); len(paths) != 0 {
		t.Fatalf("reinstalled package retained in cleanup allowlist: %v", paths)
	}
}

func TestPackageLeftoversNeverFollowLinkedFamilyOrContainer(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", local)
	root := filepath.Join(local, "Packages")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := realTemp(t)
	write(t, filepath.Join(outside, "private.dat"), []byte("keep"))
	family := filepath.Join(root, "Acme.Linked_abcdefghjkmnp")
	if err := os.Symlink(outside, family); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	query := func(string) (uint32, error) {
		t.Fatal("linked family must not reach registration query")
		return 0, nil
	}
	if paths := windowsPackageLeftovers(home, query); len(paths) != 0 {
		t.Fatalf("linked family selected: %v", paths)
	}
	if err := os.Remove(family); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if paths := windowsPackageLeftovers(home, query); len(paths) != 0 {
		t.Fatalf("linked Packages container selected: %v", paths)
	}
}
