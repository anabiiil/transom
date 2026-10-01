package scan

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Windows package family identities are Name_PublisherId, not plain app
// names. The publisher uses the 13-character Crockford Base32 alphabet;
// names follow Windows' package-string rules. Tight syntax prevents generic
// folders, container paths or a guessed vendor name becoming candidates.
var packageFamilyPattern = regexp.MustCompile(`(?i)^([a-z0-9.-]{3,50})_[a-hjkmnp-tv-z0-9]{13}$`)

func validPackageFamily(family string) bool {
	parts := packageFamilyPattern.FindStringSubmatch(family)
	if parts == nil {
		return false
	}
	name := strings.ToLower(parts[1])
	if strings.HasSuffix(name, ".") || strings.HasPrefix(name, "xn--") || strings.Contains(name, ".xn--") {
		return false
	}
	first := strings.SplitN(name, ".", 2)[0]
	if first == "con" || first == "prn" || first == "aux" || first == "nul" ||
		len(first) == 4 && (strings.HasPrefix(first, "com") || strings.HasPrefix(first, "lpt")) && first[3] >= '1' && first[3] <= '9' {
		return false
	}
	return true
}

type packageCountFunc func(family string) (uint32, error)

// windowsPackageLeftovers retains data on any unknown registration status.
// Registration is queried freshly, without caching, so callers can use the
// same candidate list both for read-only scans and cleanup validation.
func windowsPackageLeftovers(home string, registered packageCountFunc) []string {
	if registered == nil {
		return nil
	}
	root := filepath.Join(windowsAppData(home, "LOCALAPPDATA", "Local"), "Packages")
	if !plainDir(root) {
		return nil
	}
	var paths []string
	for _, path := range children(root) {
		family := filepath.Base(path)
		if !validPackageFamily(family) || !plainDir(path) {
			continue
		}
		count, err := registered(family)
		if err != nil || count != 0 || !packageHasData(path) {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// packageHasData excludes empty containers and trees of empty folders. It
// reads metadata only and skips links, placeholders and unreadable entries.
// A bounded search keeps pathological trees from delaying cleanup preflight.
func packageHasData(root string) bool {
	dirs := []string{root}
	visited := 0
	for len(dirs) > 0 {
		dir := dirs[len(dirs)-1]
		dirs = dirs[:len(dirs)-1]
		entries, err := readDirUnsorted(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if visited++; visited > 20000 {
				return false
			}
			fi, err := entry.Info()
			if err != nil || isReparse(fi) {
				continue
			}
			if fi.Mode().IsRegular() && fi.Size() > 0 && !statOf(fi).dataless {
				return true
			}
			if fi.IsDir() {
				dirs = append(dirs, filepath.Join(dir, entry.Name()))
			}
		}
	}
	return false
}
