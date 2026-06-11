package jenkins

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient builds a Client wired against the supplied test server.
// CookieJar is enabled to mirror the production httpx.New(CookieJar: true)
// configuration.
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &Client{
		BaseURL:     baseURL,
		User:        "admin",
		Pass:        "admin",
		HTTP:        &http.Client{Jar: jar, Timeout: 5 * time.Second},
		MaxAttempts: 3,
		RetryDelay:  10 * time.Millisecond,
	}
}

func TestFetchCrumbHappyPath(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crumbIssuer/api/json" && r.Method == http.MethodGet {
			atomic.AddInt32(&hits, 1)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"crumbRequestField": "Jenkins-Crumb",
				"crumb":             "deadbeef",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	cb, err := c.fetchCrumb(context.Background())
	if err != nil {
		t.Fatalf("fetchCrumb: %v", err)
	}
	if cb.Field != "Jenkins-Crumb" || cb.Value != "deadbeef" {
		t.Fatalf("unexpected crumb: %+v", cb)
	}
	// Second call must hit the cache, not the server.
	if _, err := c.fetchCrumb(context.Background()); err != nil {
		t.Fatalf("second fetchCrumb: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected 1 crumb request, got %d", got)
	}
}

func TestFetchCrumbDefaultsFieldName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"crumb":"abc"}`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	cb, err := c.fetchCrumb(context.Background())
	if err != nil {
		t.Fatalf("fetchCrumb: %v", err)
	}
	if cb.Field != "Jenkins-Crumb" {
		t.Fatalf("expected default field, got %q", cb.Field)
	}
}

func TestCreateOrUpdateJobCreatesMissingFolder(t *testing.T) {
	var (
		mu         sync.Mutex
		createdJob bool
		createdFol bool
		jobBody    string
		crumbCalls int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/crumbIssuer/api/json":
			crumbCalls++
			_ = json.NewEncoder(w).Encode(map[string]string{
				"crumbRequestField": "Jenkins-Crumb",
				"crumb":             "tok",
			})
			return

		case r.URL.Path == "/job/myfolder/api/json" && r.Method == http.MethodGet:
			if !createdFol {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"_class":"folder"}`)
			return

		case r.URL.Path == "/createItem" && r.Method == http.MethodPost:
			if r.Header.Get("Jenkins-Crumb") != "tok" {
				http.Error(w, "no crumb", http.StatusForbidden)
				return
			}
			if name := r.URL.Query().Get("name"); name != "myfolder" {
				http.Error(w, "wrong name", http.StatusBadRequest)
				return
			}
			createdFol = true
			w.WriteHeader(http.StatusOK)
			return

		case r.URL.Path == "/job/myfolder/job/myjob/api/json" && r.Method == http.MethodGet:
			if !createdJob {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			return

		case r.URL.Path == "/job/myfolder/createItem" && r.Method == http.MethodPost:
			if r.Header.Get("Jenkins-Crumb") != "tok" {
				http.Error(w, "no crumb", http.StatusForbidden)
				return
			}
			if name := r.URL.Query().Get("name"); name != "myjob" {
				http.Error(w, "wrong name", http.StatusBadRequest)
				return
			}
			body, _ := io.ReadAll(r.Body)
			jobBody = string(body)
			createdJob = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	const cfg = `<project>hello</project>`
	if err := c.CreateOrUpdateJob(context.Background(), "myfolder", "myjob", cfg); err != nil {
		t.Fatalf("CreateOrUpdateJob: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !createdFol {
		t.Fatalf("folder was not created")
	}
	if !createdJob {
		t.Fatalf("job was not created")
	}
	if !strings.Contains(jobBody, "<project>hello</project>") {
		t.Fatalf("job body mismatch: %q", jobBody)
	}
	if crumbCalls == 0 {
		t.Fatalf("expected crumb to be fetched")
	}
}

func TestCreateOrUpdateJobUpdatesExisting(t *testing.T) {
	var (
		mu         sync.Mutex
		updateCall int
		got        string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/crumbIssuer/api/json":
			_ = json.NewEncoder(w).Encode(map[string]string{"crumb": "x"})
			return
		case r.URL.Path == "/job/myjob/api/json" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			return
		case r.URL.Path == "/job/myjob/config.xml" && r.Method == http.MethodPost:
			if r.Header.Get("Jenkins-Crumb") != "x" {
				http.Error(w, "missing crumb", http.StatusForbidden)
				return
			}
			body, _ := io.ReadAll(r.Body)
			got = string(body)
			updateCall++
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if err := c.CreateOrUpdateJob(context.Background(), "", "myjob", "<project/>"); err != nil {
		t.Fatalf("CreateOrUpdateJob: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if updateCall != 1 {
		t.Fatalf("expected 1 update call, got %d", updateCall)
	}
	if !strings.Contains(got, "<project/>") {
		t.Fatalf("update body mismatch: %q", got)
	}
}

func TestStaleCrumbIsRefetched(t *testing.T) {
	var (
		mu      sync.Mutex
		crumb   = "first"
		stale   = true
		creates int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"crumbRequestField": "Jenkins-Crumb",
				"crumb":             crumb,
			})
			return
		case "/job/myjob/api/json":
			http.NotFound(w, r)
			return
		case "/createItem":
			if stale {
				stale = false
				crumb = "second"
				http.Error(w, "stale", http.StatusForbidden)
				return
			}
			if r.Header.Get("Jenkins-Crumb") != "second" {
				http.Error(w, "still stale", http.StatusForbidden)
				return
			}
			creates++
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if err := c.CreateOrUpdateJob(context.Background(), "", "myjob", "<x/>"); err != nil {
		t.Fatalf("CreateOrUpdateJob: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 {
		t.Fatalf("expected create to succeed once retry refetches crumb, got %d", creates)
	}
}

func TestRestartSafelyWaitsForHealth(t *testing.T) {
	var (
		mu         sync.Mutex
		restartHit bool
		healthyAt  = time.Now().Add(60 * time.Millisecond)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			_ = json.NewEncoder(w).Encode(map[string]string{"crumb": "z"})
			return
		case "/safeRestart":
			restartHit = true
			w.WriteHeader(http.StatusOK)
			return
		case "/login":
			if time.Now().Before(healthyAt) {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		case "/api/json":
			if time.Now().Before(healthyAt) {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	c.RetryDelay = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.RestartSafely(ctx); err != nil {
		t.Fatalf("RestartSafely: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !restartHit {
		t.Fatalf("safeRestart was not called")
	}
}

func TestRestartSafelyDeadlineExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			_ = json.NewEncoder(w).Encode(map[string]string{"crumb": "z"})
		case "/safeRestart":
			w.WriteHeader(http.StatusOK)
		default:
			// Always pretend Jenkins is still down.
			http.Error(w, "down", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	c.RetryDelay = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	err := c.RestartSafely(ctx)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestRunJobNoParams(t *testing.T) {
	var hit int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			_ = json.NewEncoder(w).Encode(map[string]string{"crumb": "tok"})
		case "/job/foo/build":
			if r.URL.Query().Get("delay") != "0sec" {
				http.Error(w, "bad delay", http.StatusBadRequest)
				return
			}
			hit++
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	if err := c.RunJob(context.Background(), "foo", nil); err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	if hit != 1 {
		t.Fatalf("expected 1 build trigger, got %d", hit)
	}
}
