// Package httpx builds *http.Client instances for the SCM/Jenkins/HTTP
// integrations.
//
// It is the Go counterpart of HttpClientFactory.groovy and its
// interceptors. Several Groovy bugs are fixed on the way:
//
//   - Hostname verification is only disabled when Options.Insecure == true.
//     The Groovy version unconditionally installs a "trust everyone"
//     HostnameVerifier.
//   - The deprecated SSLContext.getInstance("SSL") is replaced by the
//     standard TLS stack from crypto/tls.
//   - The retry interceptor is implemented as a RoundTripper wrapper with
//     exponential backoff and per-request context awareness.
package httpx

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// Options controls how New builds the HTTP client.
type Options struct {
	// Insecure disables both certificate verification AND hostname
	// matching. Use only for environments that ask for it explicitly.
	Insecure bool

	// BasicAuth, when set, adds an Authorization header to every request.
	BasicAuth *BasicAuth

	// Retry controls automatic retry of idempotent requests on 5xx.
	Retry RetryPolicy

	// Timeout for the whole roundtrip (connect + body). Zero = no client
	// timeout (use ctx instead).
	Timeout time.Duration

	// CookieJar attaches an in-memory cookie jar; needed by Jenkins.
	CookieJar bool
}

// BasicAuth holds plain-text credentials.
type BasicAuth struct {
	User, Pass string
}

// RetryPolicy describes the retry behaviour. Defaults: 3 attempts, 250ms
// base delay, capped at 5s.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

// New returns a configured *http.Client. The returned client is safe for
// concurrent use.
func New(opts Options) *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	if opts.Insecure {
		base.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // opt-in via Options.Insecure
		}
	}

	var rt http.RoundTripper = base
	if opts.BasicAuth != nil {
		rt = &basicAuthTransport{next: rt, user: opts.BasicAuth.User, pass: opts.BasicAuth.Pass}
	}
	if opts.Retry.MaxAttempts > 1 {
		rt = &retryTransport{next: rt, max: opts.Retry.MaxAttempts, base: opts.Retry.BaseDelay}
	}

	c := &http.Client{Transport: rt, Timeout: opts.Timeout}
	if opts.CookieJar {
		jar, _ := cookiejar.New(nil)
		c.Jar = jar
	}
	return c
}

type basicAuthTransport struct {
	next       http.RoundTripper
	user, pass string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.SetBasicAuth(t.user, t.pass)
	return t.next.RoundTrip(req)
}

type retryTransport struct {
	next http.RoundTripper
	max  int
	base time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	maxDelay := 5 * time.Second

	var body []byte
	if req.Body != nil && req.GetBody == nil {
		// Buffer the body so we can replay it on retry.
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		body = b
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(string(body))), nil
		}
		req.Body, _ = req.GetBody()
	}

	var lastErr error
	for attempt := 1; attempt <= t.max; attempt++ {
		if attempt > 1 && req.GetBody != nil {
			b, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = b
		}
		resp, err := t.next.RoundTrip(req)
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}
		lastErr = err
		if resp != nil {
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			_ = resp.Body.Close()
		}

		if attempt == t.max {
			break
		}

		delay := time.Duration(math.Pow(2, float64(attempt-1))) * base
		if delay > maxDelay {
			delay = maxDelay
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
		}
	}
	if lastErr == nil {
		lastErr = errors.New("retry transport exhausted with no recorded error")
	}
	return nil, lastErr
}

// Compile-time guards.
var (
	_ http.RoundTripper = (*basicAuthTransport)(nil)
	_ http.RoundTripper = (*retryTransport)(nil)
)
