//go:build browser

package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/steveyegge/gastown/internal/activity"
)

type browserTestConfig struct {
	headless bool
	slowMo   time.Duration
}

type browserAPIMock struct {
	commands        []CommandInfo
	options         OptionsResponse
	runOutput       string
	runMu           sync.Mutex
	runCommands     []string
	issueMu         sync.Mutex
	issueRequests   []IssueCreateRequest
	issueResponse   IssueCreateResponse
	sseConnections  atomic.Int32
	closeFirstSSE   bool
}

func getBrowserConfig() browserTestConfig {
	cfg := browserTestConfig{
		headless: true,
		slowMo:   0,
	}

	if os.Getenv("BROWSER_VISIBLE") == "1" {
		cfg.headless = false
		cfg.slowMo = 300 * time.Millisecond
	}

	return cfg
}

func launchBrowser(cfg browserTestConfig) (*rod.Browser, func()) {
	l := launcher.New().
		NoSandbox(true).
		Headless(cfg.headless)

	if !cfg.headless {
		l = l.Devtools(false)
	}

	u := l.MustLaunch()
	browser := rod.New().ControlURL(u).MustConnect()

	if !cfg.headless {
		browser = browser.SlowMotion(cfg.slowMo)
	}

	cleanup := func() {
		browser.MustClose()
		l.Cleanup()
	}

	return browser, cleanup
}

func newBrowserDashboardServer(t *testing.T, fetcher *MockConvoyFetcher, api *browserAPIMock) *httptest.Server {
	t.Helper()

	if fetcher == nil {
		fetcher = &MockConvoyFetcher{}
	}
	if api == nil {
		api = &browserAPIMock{}
	}
	if api.issueResponse == (IssueCreateResponse{}) {
		api.issueResponse = IssueCreateResponse{Success: true, ID: "gs-browser-1", Message: "Created issue: gs-browser-1"}
	}

	handler, err := NewConvoyHandler(fetcher, 8*time.Second, "test-token")
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Fatalf("Failed to load static assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/api/commands", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CommandListResponse{Commands: api.commands})
	})
	mux.HandleFunc("/api/options", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.options)
	})
	mux.HandleFunc("/api/run", func(w http.ResponseWriter, r *http.Request) {
		var req CommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode /api/run request: %v", err)
		}
		api.runMu.Lock()
		api.runCommands = append(api.runCommands, req.Command)
		api.runMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CommandResponse{
			Success:    true,
			Output:     api.runOutput,
			Command:    req.Command,
			DurationMs: 1,
		})
	})
	mux.HandleFunc("/api/issues/create", func(w http.ResponseWriter, r *http.Request) {
		var req IssueCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode /api/issues/create request: %v", err)
		}
		api.issueMu.Lock()
		api.issueRequests = append(api.issueRequests, req)
		api.issueMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.issueResponse)
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "flusher unavailable", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		conn := api.sseConnections.Add(1)
		_, _ = w.Write([]byte("event: connected\ndata: ok\n\n"))
		flusher.Flush()

		if api.closeFirstSSE && conn == 1 {
			time.Sleep(100 * time.Millisecond)
			return
		}

		<-r.Context().Done()
	})
	mux.Handle("/", handler)

	return httptest.NewServer(mux)
}

func waitForEval(t *testing.T, page *rod.Page, timeout time.Duration, js string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if page.MustEval(js).Bool() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("condition not satisfied within %v: %s", timeout, js)
}

func TestBrowser_ConvoyListLoads(t *testing.T) {
	fetcher := &MockConvoyFetcher{
		Convoys: []ConvoyRow{
			{
				ID:           "hq-cv-abc",
				Title:        "Feature X",
				Status:       "open",
				Progress:     "2/5",
				Completed:    2,
				Total:        5,
				LastActivity: activity.Calculate(time.Now().Add(-1 * time.Minute)),
			},
			{
				ID:           "hq-cv-def",
				Title:        "Bugfix Y",
				Status:       "closed",
				Progress:     "3/3",
				Completed:    3,
				Total:        3,
				LastActivity: activity.Calculate(time.Now().Add(-10 * time.Minute)),
			},
		},
	}

	ts := newBrowserDashboardServer(t, fetcher, &browserAPIMock{})
	defer ts.Close()

	cfg := getBrowserConfig()
	browser, cleanup := launchBrowser(cfg)
	defer cleanup()

	page := browser.MustPage(ts.URL).Timeout(30 * time.Second)
	defer page.MustClose()

	page.MustWaitLoad()

	title := page.MustElement("title").MustText()
	if !strings.Contains(title, "Gas Town") {
		t.Fatalf("Expected title to contain 'Gas Town', got: %s", title)
	}

	bodyText := page.MustElement("body").MustText()
	for _, want := range []string{"hq-cv-abc", "hq-cv-def", "Feature X", "Bugfix Y"} {
		if !strings.Contains(bodyText, want) {
			t.Errorf("Expected %q in page body", want)
		}
	}
}

func TestBrowser_SSEReconnectsAfterDisconnect(t *testing.T) {
	fetcher := &MockConvoyFetcher{
		Convoys: []ConvoyRow{
			{
				ID:           "hq-cv-live",
				Title:        "Live Convoy",
				Status:       "open",
				LastActivity: activity.Calculate(time.Now().Add(-30 * time.Second)),
			},
		},
	}
	api := &browserAPIMock{closeFirstSSE: true}

	ts := newBrowserDashboardServer(t, fetcher, api)
	defer ts.Close()

	cfg := getBrowserConfig()
	browser, cleanup := launchBrowser(cfg)
	defer cleanup()

	page := browser.MustPage(ts.URL).Timeout(30 * time.Second)
	defer page.MustClose()
	page.MustWaitLoad()

	waitForEval(t, page, 3*time.Second, `() => document.getElementById('connection-status')?.textContent === 'Live'`)
	waitForEval(t, page, 5*time.Second, `() => document.getElementById('connection-status')?.textContent === 'Reconnecting...'`)
	waitForEval(t, page, 5*time.Second, `() => document.getElementById('connection-status')?.textContent === 'Live'`)

	if api.sseConnections.Load() < 2 {
		t.Fatalf("Expected at least 2 SSE connections, got %d", api.sseConnections.Load())
	}
}

func TestBrowser_CommandPaletteFlows(t *testing.T) {
	fetcher := &MockConvoyFetcher{
		Convoys: []ConvoyRow{
			{ID: "hq-cv-palette", Status: "open"},
		},
	}
	api := &browserAPIMock{
		commands: []CommandInfo{
			{Name: "status", Desc: "Show town status", Category: "Status"},
			{Name: "mail send", Desc: "Send message", Category: "Mail", Args: "<address> -s <subject> -m <message>", ArgType: "agents"},
		},
		options: OptionsResponse{
			Agents: []OptionItem{
				{Name: "gastown/witness", Running: true},
			},
		},
		runOutput: "town healthy",
	}

	ts := newBrowserDashboardServer(t, fetcher, api)
	defer ts.Close()

	cfg := getBrowserConfig()
	browser, cleanup := launchBrowser(cfg)
	defer cleanup()

	page := browser.MustPage(ts.URL).Timeout(30 * time.Second)
	defer page.MustClose()
	page.MustWaitLoad()

	page.MustElement("#open-palette-btn").MustClick()
	waitForEval(t, page, 2*time.Second, `() => document.getElementById('command-palette-overlay')?.classList.contains('open')`)

	page.MustElement("#command-palette-input").MustInput("status")
	waitForEval(t, page, 2*time.Second, `() => document.querySelectorAll('.command-item').length >= 1`)
	page.MustElement(".command-item").MustClick()

	waitForEval(t, page, 3*time.Second, `() => document.getElementById('output-panel')?.classList.contains('open')`)
	waitForEval(t, page, 2*time.Second, `() => document.getElementById('output-panel-content')?.textContent.includes('town healthy')`)

	api.runMu.Lock()
	if len(api.runCommands) == 0 || api.runCommands[0] != "status" {
		t.Fatalf("Expected palette to run status, got %v", api.runCommands)
	}
	api.runMu.Unlock()

	page.MustElement("#open-palette-btn").MustClick()
	waitForEval(t, page, 2*time.Second, `() => document.getElementById('command-palette-overlay')?.classList.contains('open')`)

	page.MustElement("#command-palette-input").MustInput("mail send")
	waitForEval(t, page, 2*time.Second, `() => Array.from(document.querySelectorAll('.command-item')).some(el => el.textContent.includes('mail send'))`)
	page.MustElement(".command-item").MustClick()

	waitForEval(t, page, 2*time.Second, `() => document.querySelector('.command-args-header')?.textContent.includes('gt mail send')`)
	waitForEval(t, page, 2*time.Second, `() => document.getElementById('arg-field-2')?.tagName === 'TEXTAREA'`)
}

func TestBrowser_IssueModalCreateFlow(t *testing.T) {
	fetcher := &MockConvoyFetcher{
		Issues: []IssueRow{
			{ID: "gs-1", Title: "Existing issue", Type: "task", Priority: 2},
		},
	}
	api := &browserAPIMock{
		issueResponse: IssueCreateResponse{Success: true, ID: "gs-new-7", Message: "Created issue: gs-new-7"},
	}

	ts := newBrowserDashboardServer(t, fetcher, api)
	defer ts.Close()

	cfg := getBrowserConfig()
	browser, cleanup := launchBrowser(cfg)
	defer cleanup()

	page := browser.MustPage(ts.URL).Timeout(30 * time.Second)
	defer page.MustClose()
	page.MustWaitLoad()

	page.MustElement(".new-issue-btn").MustClick()
	waitForEval(t, page, 2*time.Second, `() => document.getElementById('issue-modal')?.style.display === 'flex'`)

	page.MustElement("#issue-title").MustInput("Dashboard modal flow")
	page.MustElement("#issue-priority").MustSelect("3")
	page.MustElement("#issue-description").MustInput("Verify modal submit resets state")
	page.MustElement("#issue-submit-btn").MustClick()

	waitForEval(t, page, 3*time.Second, `() => document.getElementById('issue-modal')?.style.display === 'none'`)
	waitForEval(t, page, 3*time.Second, `() => document.getElementById('toast-container')?.textContent.includes('gs-new-7')`)
	waitForEval(t, page, 2*time.Second, `() => document.getElementById('issue-title')?.value === ''`)

	api.issueMu.Lock()
	defer api.issueMu.Unlock()
	if len(api.issueRequests) != 1 {
		t.Fatalf("Expected exactly one create request, got %d", len(api.issueRequests))
	}
	req := api.issueRequests[0]
	if req.Title != "Dashboard modal flow" {
		t.Fatalf("Title = %q, want %q", req.Title, "Dashboard modal flow")
	}
	if req.Priority != 3 {
		t.Fatalf("Priority = %d, want 3", req.Priority)
	}
	if req.Description != "Verify modal submit resets state" {
		t.Fatalf("Description = %q, want expected body", req.Description)
	}
}
