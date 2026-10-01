//go:build !windows

// Package winapp owns Transom's native Windows WebView2 window.
package winapp

import (
	"context"
	"errors"
)

func Run(context.Context, string) error {
	return errors.New("the Windows desktop window is only available on Windows")
}

// ShowError displays errors in GUI builds, which have no console.
func ShowError(error) {}
