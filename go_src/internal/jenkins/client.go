// Package jenkins implements a thin Jenkins REST adapter, replacing the
// Groovy infrastructure/jenkins package (JenkinsApiClient, JobManager,
// UserManager, GlobalPropertyManager, PrometheusConfigurator).
//
// Differences vs. the Groovy original:
//
//   - The CSRF crumb is fetched lazily on the first mutating request, cached
//     on the Client, and re-fetched automatically when a request returns
//     401/403. The Groovy code forgets the crumb in a handful of edge cases.
//   - RestartSafely polls a real Jenkins health endpoint until it returns 200
//     instead of a fixed Thread.sleep.
//   - The HTTP client is expected to come from internal/httpx with
//     CookieJar: true, so Jenkins session cookies are reused across calls.
package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client talks to a single Jenkins instance.
//
// HTTP must be built with internal/httpx.New using CookieJar: true so that
// the JSESSIONID stays attached to the crumb that issued it.
type Client struct {
	BaseURL string
	User    string
	Pass    string
	HTTP    *http.Client

	// MaxAttempts caps the per-call retry loop for 401/403/5xx responses.
	// Defaults to 5 when zero.
	MaxAttempts int
	// RetryDelay is the sleep between retries. Defaults to 500ms.
	RetryDelay time.Duration

	mu    sync.Mutex
	crumb *crumb
}

type crumb struct {
	Field string `json:"crumbRequestField"`
	Value string `json:"crumb"`
}

// resolve joins the client BaseURL with a relative path.
func (c *Client) resolve(rel string) (string, error) {
	if c.BaseURL == "" {
		return "", errors.New("jenkins: BaseURL is empty")
	}
	rel = strings.TrimLeft(rel, "/")
	base := strings.TrimRight(c.BaseURL, "/")
	return base + "/" + rel, nil
}

// fetchCrumb retrieves and caches the CSRF crumb from
// /crumbIssuer/api/json. The cookie jar attached to HTTP is required so the
// session that issued the crumb stays sticky for the next request.
func (c *Client) fetchCrumb(ctx context.Context) (*crumb, error) {
	c.mu.Lock()
	if c.crumb != nil {
		out := c.crumb
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	u, err := c.resolve("crumbIssuer/api/json")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jenkins: crumb request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jenkins: crumb request returned %d: %s", resp.StatusCode, truncate(body, 200))
	}
	var got crumb
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return nil, fmt.Errorf("jenkins: decode crumb json: %w", err)
	}
	if got.Value == "" {
		return nil, errors.New("jenkins: crumb response missing crumb field")
	}
	if got.Field == "" {
		got.Field = "Jenkins-Crumb"
	}

	c.mu.Lock()
	c.crumb = &got
	c.mu.Unlock()
	return &got, nil
}

// invalidateCrumb forces the next mutating call to fetch a fresh crumb.
// This is the bug-fix path: the Groovy client kept stale crumbs across
// Jenkins restarts which broke later requests.
func (c *Client) invalidateCrumb() {
	c.mu.Lock()
	c.crumb = nil
	c.mu.Unlock()
}

// applyAuth attaches basic-auth credentials when set. The Authorization
// header injected by httpx.New is sufficient in production; setting it here
// explicitly makes the helper usable with a plain *http.Client (e.g. in
// tests).
func (c *Client) applyAuth(req *http.Request) {
	if c.User != "" || c.Pass != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}
}

// doRequest performs an HTTP request, attaching a fresh crumb when needed
// and transparently retrying 401/403 responses (stale crumb / session) and
// 5xx blips. Always closes the response body on error paths.
func (c *Client) doRequest(ctx context.Context, method, rel string, body io.Reader, contentType string, mutating bool) (*http.Response, []byte, error) {
	maxAttempts := c.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	delay := c.RetryDelay
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}

	u, err := c.resolve(rel)
	if err != nil {
		return nil, nil, err
	}

	// Buffer the body once so each retry attempt can replay it.
	var raw []byte
	if body != nil {
		raw, err = io.ReadAll(body)
		if err != nil {
			return nil, nil, fmt.Errorf("jenkins: read request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var reqBody io.Reader
		if raw != nil {
			reqBody = strings.NewReader(string(raw))
		}
		req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
		if err != nil {
			return nil, nil, err
		}
		c.applyAuth(req)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if mutating {
			cb, err := c.fetchCrumb(ctx)
			if err != nil {
				return nil, nil, err
			}
			req.Header.Set(cb.Field, cb.Value)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if !sleepWithCtx(ctx, delay) {
				return nil, nil, ctx.Err()
			}
			continue
		}

		// 401/403 typically means stale crumb or restarted Jenkins.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			drain(resp)
			c.invalidateCrumb()
			lastErr = fmt.Errorf("jenkins: status %d", resp.StatusCode)
			if attempt == maxAttempts {
				return nil, nil, lastErr
			}
			if !sleepWithCtx(ctx, delay) {
				return nil, nil, ctx.Err()
			}
			continue
		}
		if resp.StatusCode >= 500 {
			drain(resp)
			lastErr = fmt.Errorf("jenkins: status %d", resp.StatusCode)
			if attempt == maxAttempts {
				return nil, nil, lastErr
			}
			if !sleepWithCtx(ctx, delay) {
				return nil, nil, ctx.Err()
			}
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("jenkins: read response body: %w", readErr)
		}
		return resp, respBody, nil
	}

	if lastErr == nil {
		lastErr = errors.New("jenkins: request failed without recorded error")
	}
	return nil, nil, lastErr
}

// runScript executes a Groovy snippet via /scriptText. The string body
// matches Jenkins' expected return value (typically printed by the script).
func (c *Client) runScript(ctx context.Context, script string) (string, error) {
	form := url.Values{}
	form.Set("script", script)
	resp, body, err := c.doRequest(
		ctx,
		http.MethodPost,
		"scriptText",
		strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded",
		true,
	)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jenkins: scriptText returned %d: %s", resp.StatusCode, truncate(body, 200))
	}
	return string(body), nil
}

// drain reads-and-closes a response body so the underlying connection can
// be reused.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// sleepWithCtx sleeps for d or returns false if the context is cancelled
// first.
func sleepWithCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
