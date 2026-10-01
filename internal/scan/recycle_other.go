//go:build !windows

package scan

import "context"

func scanWindowsRecycleBin(context.Context, *Env) ([]Item, error) { return nil, nil }
