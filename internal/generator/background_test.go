package generator

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/wild-sergunys/traffic-gen/internal/config"
)

// The web profile must preserve the base URL and transmit ordinary text losslessly.
func TestWebBackground(t *testing.T) {
	tests := []struct{ method, path, field, value string }{
		{"GET", "/health", "check", "ready"},
		{"GET", "/search", "q", "temperature sensor"},
		{"GET", "/search", "q", "O'Reilly maintenance manual"},
		{"GET", "/search", "q", "pressure < 10 & temperature > 20"},
		{"POST", "/feedback", "message", "Pump A&B: efficiency 95% + spare parts"},
		{"POST", "/feedback", "message", `Operator said "restart after inspection".`},
		{"GET", "/docs", "q", "HTML <div> and SQL SELECT examples"},
		{"POST", "/search", "q", "датчик температуры №7"},
	}
	var mu sync.Mutex
	var received int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		want := tests[received%len(tests)]
		received++
		if r.Method != want.method || r.URL.EscapedPath() != "/app%2Fv1"+want.path || r.URL.Query().Get("device") != "7" {
			t.Errorf("method/URL = %s %s", r.Method, r.URL)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get(want.field) != want.value {
			t.Errorf("%s = %q, want %q", want.field, r.Form.Get(want.field), want.value)
		}
		if r.Header.Get("User-Agent") != "test-UA" {
			t.Error("missing User-Agent")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	cfg := config.Config{Target: server.URL + "/app%2Fv1/?device=7", Mode: "http1", Profile: "web", Workers: 1, Duration: time.Second, RequestTimeout: time.Second}
	g, err := New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.client.CloseIdleConnections)
	for range 2 * len(tests) {
		_, status, err := g.send(t.Context(), "test-UA")
		if err != nil || status != http.StatusNoContent {
			t.Fatalf("status/error = %d/%v", status, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if received != 2*len(tests) {
		t.Fatalf("requests = %d", received)
	}
	if g.target.String() != cfg.Target {
		t.Error("base target changed")
	}
}

func TestWebBackgroundConcurrent(t *testing.T) {
	cfg := config.Config{Target: "http://example.com/", Mode: "http1", Profile: "web", Workers: 4}
	g, err := New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	keys := make(chan string, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			req, err := g.newRequest(t.Context(), "ua")
			if err != nil {
				t.Error(err)
				return
			}
			defer req.Body.Close()
			if err := req.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			keys <- req.Method + req.URL.Path + req.Form.Encode()
		})
	}
	wg.Wait()
	close(keys)
	counts := make(map[string]int)
	for key := range keys {
		counts[key]++
	}
	if len(counts) != 8 {
		t.Fatalf("unique requests = %d, want 8", len(counts))
	}
	for key, count := range counts {
		if count != 4 {
			t.Errorf("request %q count = %d, want 4", key, count)
		}
	}
}
