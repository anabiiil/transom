package clean

import (
	"context"
	"fmt"
	"runtime"

	"transom/internal/safety"
)

// Check also protects installations and editor data, regardless of the scan
// category or whether an old scan listed the path as disposable.
func (g Guard) Check(path string) (string, error) {
	return g.CheckContext(context.Background(), path)
}

func (g Guard) CheckContext(ctx context.Context, path string) (string, error) {
	if safety.ProtectedLocation(path, g.Home, runtime.GOOS) {
		return "", fmt.Errorf("refusing to remove an application or editor data: %s", path)
	}
	target, err := g.checkPath(path)
	if err != nil {
		return "", err
	}
	if err := safety.CheckCleanup(ctx, target, g.Home, runtime.GOOS); err != nil {
		return "", err
	}
	return target, nil
}
