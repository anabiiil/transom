package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"transom/internal/clean"
	"transom/internal/scan"
)

const testToken = "tok123"

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

func newTestServer(t *testing.T, s *server) *httptest.Server {
	t.Helper()
	if s == nil {
		s = newServer()
	}
	ts := httptest.NewServer(newHandlerWith(testToken, s))
	t.Cleanup(ts.Close)
	return ts
}

func post(t *testing.T, ts *httptest.Server, path, token, body string) (int, envelope) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set(TokenHeader, token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env envelope
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &env)
	return resp.StatusCode, env
}

func TestTokenRequired(t *testing.T) {
	ts := newTestServer(t, nil)
	for _, tok := range []string{"", "wrong", testToken + "x"} {
		code, env := post(t, ts, "/api/categories", tok, "{}")
		if code != http.StatusForbidden || env.OK {
			t.Fatalf("token %q: code %d env %+v", tok, code, env)
		}
	}
}

func TestCategoriesEnvelope(t *testing.T) {
	ts := newTestServer(t, nil)
	code, env := post(t, ts, "/api/categories", testToken, "{}")
	if code != http.StatusOK || !env.OK {
		t.Fatalf("code %d env %+v", code, env)
	}
	var cats []scan.Category
	if err := json.Unmarshal(env.Data, &cats); err != nil {
		t.Fatal(err)
	}
	if len(cats) != len(scan.Categories()) || cats[0].ID != "user-caches" {
		t.Fatalf("categories = %+v", cats)
	}
	// Empty body is fine too.
	if code, env := post(t, ts, "/api/categories", testToken, ""); code != 200 || !env.OK {
		t.Fatalf("empty body: %d %+v", code, env)
	}
}

func TestErrorsUseEnvelope(t *testing.T) {
	ts := newTestServer(t, nil)
	code, env := post(t, ts, "/api/scan/status", testToken, `{"jobId":"nope"}`)
	if code == http.StatusOK || env.OK || env.Error == "" {
		t.Fatalf("code %d env %+v", code, env)
	}
	_, env = post(t, ts, "/api/scan/start", testToken, `{"categories":["bogus"]}`)
	if env.OK || !strings.Contains(env.Error, "unknown category") {
		t.Fatalf("env %+v", env)
	}
	_, env = post(t, ts, "/api/clean", testToken, `{"items":["x"]}`)
	if env.OK || !strings.Contains(env.Error, "scan") {
		t.Fatalf("clean without scan: %+v", env)
	}
	_, env = post(t, ts, "/api/scan/start", testToken, `{not json`)
	if env.OK {
		t.Fatal("bad JSON accepted")
	}
}

func TestMethodAndHostChecks(t *testing.T) {
	ts := newTestServer(t, nil)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/categories", nil)
	req.Header.Set(TokenHeader, testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET api: %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/categories", strings.NewReader("{}"))
	req.Header.Set(TokenHeader, testToken)
	req.Host = "evil.example:80"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("rebinding host: %d", resp.StatusCode)
	}

	for _, h := range []string{"127.0.0.1:1234", "localhost:1", "LOCALHOST", "127.0.0.1"} {
		if !hostAllowed(h) {
			t.Errorf("%s refused", h)
		}
	}
	for _, h := range []string{"evil.com", "127.0.0.1.evil.com", "0.0.0.0:80", "[::1]:80", ""} {
		if hostAllowed(h) {
			t.Errorf("%s allowed", h)
		}
	}
}

func TestStaticFiles(t *testing.T) {
	ts := newTestServer(t, nil)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("/: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, p := range []string{"/static/../ui.go", "/static/", "/static/nope.js", "/other"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, resp.StatusCode)
		}
	}
	// Whatever assets exist are served with the right type.
	entries, _ := webFS.ReadDir("web")
	for _, e := range entries {
		if e.IsDir() || e.Name() == "index.html" {
			continue
		}
		resp, err := http.Get(ts.URL + "/static/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := contentTypes[strings.ToLower(filepath.Ext(e.Name()))]
		if resp.StatusCode != 200 || (want != "" && resp.Header.Get("Content-Type") != want) {
			t.Errorf("%s: %d %q", e.Name(), resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
}

func fakeResult() *scan.Result {
	return &scan.Result{
		ScannedAt: "2026-09-26T00:00:00Z",
		TotalSize: 30,
		Categories: []scan.CategoryResult{{ID: "user-caches", TotalSize: 30, Count: 2, Items: []scan.Item{
			{ID: "i1", Path: "/Users/x/Library/Caches/a", Size: 20, Kind: scan.KindDir, Risk: scan.RiskSafe},
			{ID: "i2", Path: "/Users/x/Library/Caches/b", Size: 10, Kind: scan.KindDir, Risk: scan.RiskSafe},
		}}},
	}
}

func TestScanJobAndClean(t *testing.T) {
	release := make(chan struct{})
	s := newServer()
	s.scanFn = func(ctx context.Context, ids []string, opts scan.Options, prog *scan.Progress) (*scan.Result, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return fakeResult(), nil
	}
	var gotReq clean.Request
	s.cleanFn = func(ctx context.Context, res *scan.Result, req clean.Request) (*clean.Result, error) {
		gotReq = req
		return &clean.Result{Freed: 20, Removed: 1, Failed: []clean.Failure{}, RemovedIDs: []string{"i1"}}, nil
	}
	ts := newTestServer(t, s)

	_, env := post(t, ts, "/api/scan/start", testToken, `{"categories":[],"options":{"staleDays":30,"roots":["~/code"]}}`)
	if !env.OK {
		t.Fatalf("start: %+v", env)
	}
	var started struct{ JobID string }
	json.Unmarshal(env.Data, &started)
	if started.JobID != "s1" {
		t.Fatalf("jobId = %q", started.JobID)
	}
	// One scan at a time.
	if _, env := post(t, ts, "/api/scan/start", testToken, `{}`); env.OK {
		t.Fatal("second concurrent scan accepted")
	}
	_, env = post(t, ts, "/api/scan/status", testToken, `{"jobId":"s1"}`)
	var st struct {
		State    string          `json:"state"`
		Progress map[string]any  `json:"progress"`
		Result   json.RawMessage `json:"result"`
	}
	json.Unmarshal(env.Data, &st)
	if st.State != "running" || st.Result != nil {
		t.Fatalf("status while running: %s", env.Data)
	}
	for _, k := range []string{"category", "scanned", "found", "elapsedMs"} {
		if _, ok := st.Progress[k]; !ok {
			t.Fatalf("progress lacks %s: %s", k, env.Data)
		}
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for st.State == "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		_, env = post(t, ts, "/api/scan/status", testToken, `{"jobId":"s1"}`)
		st.Result = nil
		json.Unmarshal(env.Data, &st)
	}
	if st.State != "done" || st.Result == nil {
		t.Fatalf("final status: %s", env.Data)
	}

	// Reveal only accepts item paths of the last scan.
	if _, env := post(t, ts, "/api/reveal", testToken, `{"path":"/etc/passwd"}`); env.OK {
		t.Fatal("reveal of a non-item path accepted")
	}

	_, env = post(t, ts, "/api/clean", testToken, `{"items":["i1"],"mode":"trash","dryRun":false}`)
	if !env.OK {
		t.Fatalf("clean: %+v", env)
	}
	if len(gotReq.Items) != 1 || gotReq.Items[0] != "i1" || gotReq.Mode != "trash" {
		t.Fatalf("clean request = %+v", gotReq)
	}
	var cr map[string]any
	json.Unmarshal(env.Data, &cr)
	for _, k := range []string{"freed", "removed", "dryRun", "failed"} {
		if _, ok := cr[k]; !ok {
			t.Fatalf("clean result lacks %s: %s", k, env.Data)
		}
	}
	s.mu.Lock()
	latest := s.latest
	s.mu.Unlock()
	if _, ok := latest.Lookup("i1"); ok || latest.TotalSize != 10 {
		t.Fatalf("stored scan not updated after clean: %+v", latest)
	}
}

func TestScanCancel(t *testing.T) {
	s := newServer()
	s.scanFn = func(ctx context.Context, ids []string, opts scan.Options, prog *scan.Progress) (*scan.Result, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ts := newTestServer(t, s)
	post(t, ts, "/api/scan/start", testToken, `{}`)
	if _, env := post(t, ts, "/api/scan/cancel", testToken, `{"jobId":"s1"}`); !env.OK || string(env.Data) != "{}" {
		t.Fatalf("cancel: %+v", env)
	}
	var st struct{ State string }
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, env := post(t, ts, "/api/scan/status", testToken, `{"jobId":"s1"}`)
		json.Unmarshal(env.Data, &st)
		if st.State != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st.State != "cancelled" {
		t.Fatalf("state = %q", st.State)
	}
}

func TestPrefsAndHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	ts := newTestServer(t, nil)
	if _, env := post(t, ts, "/api/prefs/set", testToken, `{"key":"theme","value":"dark"}`); !env.OK {
		t.Fatalf("set: %+v", env)
	}
	_, env := post(t, ts, "/api/prefs/get", testToken, `{}`)
	var prefs map[string]string
	json.Unmarshal(env.Data, &prefs)
	if prefs["theme"] != "dark" {
		t.Fatalf("prefs = %s", env.Data)
	}
	_, env = post(t, ts, "/api/history", testToken, `{}`)
	if !env.OK || !bytes.Equal(bytes.TrimSpace(env.Data), []byte("[]")) {
		t.Fatalf("history = %+v", env)
	}
}

func TestWindowsPageUsesOriginalAssetsAndWindowsCopy(t *testing.T) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(platformHTML(b, "windows"))
	for _, want := range []string{`data-platform="windows"`, "disk cleanup for Windows", "/static/app.css", "/static/app.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(page, "{{") || strings.Contains(page, "macOS") {
		t.Fatal("unresolved platform content")
	}
}

func TestDesktopOwnsPanelLifetime(t *testing.T) {
	var panelURL string
	wantErr := errors.New("window closed with an error")
	err := RunDesktop(context.Background(), func(ctx context.Context, address string) error {
		panelURL = address
		u, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		body := strings.NewReader("{}")
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u.Scheme+"://"+u.Host+"/api/categories", body)
		req.Header.Set(TokenHeader, u.Query().Get("t"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("desktop API returned %d", resp.StatusCode)
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("window error lost: %v", err)
	}
	client := &http.Client{Timeout: time.Second}
	if resp, err := client.Get(panelURL); err == nil {
		resp.Body.Close()
		t.Fatal("panel server still running after desktop window closed")
	}
}

func TestExpandRootsHomeAndRelativePaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	roots, err := expandRoots([]string{"~", "~/Projects"})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 || roots[0] != home || roots[1] != filepath.Join(home, "Projects") {
		t.Fatalf("roots = %v", roots)
	}
	if _, err := expandRoots([]string{"Projects"}); err == nil {
		t.Fatal("relative root accepted")
	}
}

func TestDisk(t *testing.T) {
	ts := newTestServer(t, nil)
	_, env := post(t, ts, "/api/disk", testToken, `{}`)
	var d DiskInfo
	json.Unmarshal(env.Data, &d)
	if !env.OK || d.Total <= 0 || d.Free <= 0 || d.Used != d.Total-d.Free || d.Volume == "" {
		t.Fatalf("disk = %+v %+v", env, d)
	}
}

func TestRunHostPrintsURLAndStopsOnEOF(t *testing.T) {
	var out bytes.Buffer
	done := make(chan error, 1)
	stdin, w := io.Pipe()
	go func() { done <- RunHost(context.Background(), stdin, &out) }()
	time.Sleep(200 * time.Millisecond)
	w.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunHost didn't return on stdin EOF")
	}
	line := out.String()
	if !strings.HasPrefix(line, "TRANSOM_UI_URL=http://127.0.0.1:") || !strings.Contains(line, "/?t=") ||
		strings.Count(line, "\n") != 1 {
		t.Fatalf("output %q", line)
	}
}
