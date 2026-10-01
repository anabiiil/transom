package clean

// Guard decides whether a path may be removed. See Check.
type Guard struct {
	Home      string   // the user's home directory
	TempRoots []string // additional allowed roots (temp dirs)
	UserRoots []string // Windows Known Folders, protected themselves but allow contents

	// ProjectRoots are extra roots (e.g. an external projects volume)
	// under which ONLY dependency folders may be removed — see
	// checkProjectRoot. Clean sets them for stale-deps items only.
	ProjectRoots []string

	// CacheRoots are approved platform cleanup locations: caches and
	// explicitly reviewed orphan package data. Run rechecks the scanner
	// allowlist before permitting these targets inside AppData.
	CacheRoots []string

	// RequireCacheRoot prevents generic home/temp/user grants from overriding
	// a dynamic allowlist, such as freshly revalidated Windows package leftovers.
	RequireCacheRoot bool
}

// DepDirNames are the only base names removable under a project root.
var DepDirNames = map[string]bool{"node_modules": true, "vendor": true, ".venv": true, "target": true}
