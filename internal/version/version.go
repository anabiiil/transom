// Package version is the single source of truth for Transom's version.
package version

// Number is the release version. It's a var (not a const) so release
// builds can stamp it: -ldflags "-X transom/internal/version.Number=1.2.3".
var Number = "0.2.3"
