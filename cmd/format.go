package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"transom/internal/scan"
)

// humanBytes formats n in base 1000, like Finder ("1.2 GB").
func humanBytes(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	for _, unit := range []string{"kB", "MB", "GB", "TB"} {
		f /= 1000
		if f < 1000 || unit == "TB" {
			if f >= 100 {
				return fmt.Sprintf("%.0f %s", f, unit)
			}
			return fmt.Sprintf("%.1f %s", f, unit)
		}
	}
	return fmt.Sprintf("%d B", n)
}

// isTerminal reports whether f is a character device (an interactive tty).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// scanWithProgress runs a scan, drawing a one-line progress indicator on
// stderr when it's a terminal.
func scanWithProgress(ctx context.Context, ids []string, opts scan.Options) (*scan.Result, time.Duration, error) {
	prog := scan.NewProgress()
	start := time.Now()
	stop := make(chan struct{})
	drawn := make(chan struct{})
	tty := isTerminal(os.Stderr)
	go func() {
		defer close(drawn)
		if !tty {
			<-stop
			return
		}
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				fmt.Fprint(os.Stderr, "\r\033[K")
				return
			case <-t.C:
				s := prog.Snapshot()
				name := s.Category
				if c, ok := scan.LookupCategory(s.Category); ok {
					name = c.Name
				}
				fmt.Fprintf(os.Stderr, "\r\033[KScanning %s… %d entries, %d found", name, s.Scanned, s.Found)
			}
		}
	}()
	res, err := scan.Run(ctx, ids, opts, prog)
	close(stop)
	<-drawn
	return res, time.Since(start), err
}

// splitIDs flattens repeated/comma-separated --category values.
func splitIDs(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, id := range strings.Split(v, ",") {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}
