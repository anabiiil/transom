package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"transom/internal/clean"
	"transom/internal/config"
	"transom/internal/scan"
)

// keepJobs bounds how many finished scan jobs are remembered.
const keepJobs = 8

// job is one scan run.
type job struct {
	id     string
	cancel context.CancelFunc
	prog   *scan.Progress

	// guarded by server.mu
	state  string // running | done | error | cancelled
	err    string
	result *scan.Result
}

// server holds the panel's state: scan jobs and the latest result,
// which clean and reveal resolve against.
type server struct {
	mu      sync.Mutex
	seq     int
	jobs    map[string]*job
	order   []string
	active  *job
	latest  *scan.Result
	cleanMu sync.Mutex // one clean at a time

	// scanFn runs a scan; tests may stub it.
	scanFn func(ctx context.Context, ids []string, opts scan.Options, prog *scan.Progress) (*scan.Result, error)
	// cleanFn runs a clean; tests may stub it.
	cleanFn func(ctx context.Context, res *scan.Result, req clean.Request) (*clean.Result, error)
}

func newServer() *server {
	return &server{jobs: map[string]*job{}, scanFn: scan.Run, cleanFn: clean.Run}
}

func decode(body []byte, v any) error {
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("bad request body: %v", err)
	}
	return nil
}

func (s *server) register(api func(string, apiFunc)) {
	api("/api/disk", func(ctx context.Context, _ []byte) (any, error) { return diskInfo() })
	api("/api/categories", func(ctx context.Context, _ []byte) (any, error) { return scan.Categories(), nil })
	api("/api/scan/start", s.scanStart)
	api("/api/scan/status", s.scanStatus)
	api("/api/scan/cancel", s.scanCancel)
	api("/api/clean", s.clean)
	api("/api/reveal", s.reveal)
	api("/api/history", func(ctx context.Context, _ []byte) (any, error) { return config.History() })
	api("/api/prefs/get", func(ctx context.Context, _ []byte) (any, error) { return config.Prefs() })
	api("/api/prefs/set", func(ctx context.Context, body []byte) (any, error) {
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := decode(body, &req); err != nil {
			return nil, err
		}
		return nil, config.SetPref(req.Key, req.Value)
	})
}

// expandRoots turns "~/x" into an absolute path and rejects relative ones.
func expandRoots(roots []string) ([]string, error) {
	var out []string
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if r == "~" || strings.HasPrefix(r, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			r = filepath.Join(home, strings.TrimPrefix(r, "~"))
		}
		if !filepath.IsAbs(r) {
			return nil, fmt.Errorf("root must be an absolute path: %s", r)
		}
		out = append(out, filepath.Clean(r))
	}
	return out, nil
}

func (s *server) scanStart(_ context.Context, body []byte) (any, error) {
	var req struct {
		Categories []string     `json:"categories"`
		Options    scan.Options `json:"options"`
	}
	if err := decode(body, &req); err != nil {
		return nil, err
	}
	for _, id := range req.Categories {
		if _, ok := scan.LookupCategory(id); !ok {
			return nil, fmt.Errorf("unknown category %q", id)
		}
	}
	roots, err := expandRoots(req.Options.Roots)
	if err != nil {
		return nil, err
	}
	req.Options.Roots = roots

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		return nil, errors.New("a scan is already running")
	}
	s.seq++
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{id: fmt.Sprintf("s%d", s.seq), cancel: cancel, prog: scan.NewProgress(), state: "running"}
	s.jobs[j.id] = j
	s.order = append(s.order, j.id)
	for len(s.order) > keepJobs {
		delete(s.jobs, s.order[0])
		s.order = s.order[1:]
	}
	s.active = j

	go func() {
		res, err := s.scanFn(ctx, req.Categories, req.Options, j.prog)
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case ctx.Err() != nil:
			j.state = "cancelled"
		case err != nil:
			j.state, j.err = "error", err.Error()
		default:
			j.state, j.result = "done", res
			s.latest = res
		}
		cancel()
		if s.active == j {
			s.active = nil
		}
	}()
	return map[string]string{"jobId": j.id}, nil
}

type scanStatus struct {
	State    string        `json:"state"`
	Progress scan.Snapshot `json:"progress"`
	Error    string        `json:"error,omitempty"`
	Result   *scan.Result  `json:"result,omitempty"`
}

func (s *server) jobFor(body []byte) (*job, error) {
	var req struct {
		JobID string `json:"jobId"`
	}
	if err := decode(body, &req); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[req.JobID]
	if !ok {
		return nil, fmt.Errorf("unknown scan job %q", req.JobID)
	}
	return j, nil
}

func (s *server) scanStatus(_ context.Context, body []byte) (any, error) {
	j, err := s.jobFor(body)
	if err != nil {
		return nil, err
	}
	snap := j.prog.Snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := scanStatus{State: j.state, Progress: snap, Error: j.err}
	if j.state == "done" {
		st.Result = j.result
	}
	return st, nil
}

func (s *server) scanCancel(_ context.Context, body []byte) (any, error) {
	j, err := s.jobFor(body)
	if err != nil {
		return nil, err
	}
	j.cancel()
	return nil, nil
}

func (s *server) clean(ctx context.Context, body []byte) (any, error) {
	var req clean.Request
	if err := decode(body, &req); err != nil {
		return nil, err
	}
	s.cleanMu.Lock()
	defer s.cleanMu.Unlock()
	s.mu.Lock()
	latest := s.latest
	s.mu.Unlock()
	if latest == nil {
		return nil, errors.New("no scan result: run a scan first")
	}
	// Commands and moves shouldn't die with the HTTP request.
	res, err := s.cleanFn(context.WithoutCancel(ctx), latest, req)
	if err != nil {
		return nil, err
	}
	if !res.DryRun && len(res.RemovedIDs) > 0 {
		s.mu.Lock()
		if s.latest == latest {
			s.latest = latest.Without(res.RemovedIDs)
		}
		s.mu.Unlock()
	}
	return res, nil
}

func (s *server) reveal(_ context.Context, body []byte) (any, error) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decode(body, &req); err != nil {
		return nil, err
	}
	s.mu.Lock()
	ok := s.latest.HasPath(req.Path)
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("not an item of the last scan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "/usr/bin/open", "-R", req.Path).Run(); err != nil {
		return nil, fmt.Errorf("reveal in Finder: %v", err)
	}
	return nil, nil
}
