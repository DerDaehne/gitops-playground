// Package scmmanager is the Go counterpart of
// infrastructure/git/providers/scmmanager/* in the Groovy code.
//
// The Groovy code used Retrofit + OkHttp + Jackson; we use net/http +
// encoding/json directly. The API base URL is expected to end in /api/
// (i.e. ".../scm/api/"); endpoint paths are joined relative to that base.
//
// Bug fixes vs. the Groovy original (also called out in the package
// documentation of internal/scm):
//
//   - Plugin-install polling respects ctx.Done() instead of busy-looping
//     until a hard-coded timeout (ScmManagerSetup.groovy lines 22-40).
//   - handle201or409 returned a useless error string when errorBody was
//     nil (Groovy NPE'd). We always include the HTTP status text and the
//     response body if any.
//   - SplitRepoTarget is validated up front; the Groovy code would crash
//     on a missing slash with an ArrayIndexOutOfBoundsException.
package scmmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// Config is the subset of ScmManagerConfig we actually need to talk to a
// running SCM-Manager.
type Config struct {
	// APIBase must end with "/api/" (e.g.
	// "http://scmm.scm-manager.svc.cluster.local/scm/api/").
	APIBase string
	// ClientBase / InClusterBase let the provider answer RepoURL(...)
	// without having to re-derive them.
	ClientBase    string
	InClusterBase string
	// NamePrefix is the optional tenant prefix used by RepoURL helpers.
	NamePrefix string
	// GitOpsUsername is the technical user used by ArgoCD / Jenkins.
	GitOpsUsername string
}

// Client talks to a single SCM-Manager instance. It is the Go equivalent
// of ScmManagerApiClient + ScmManager in the Groovy code, minus the
// HelmStrategy bootstrap.
type Client struct {
	cfg   Config
	http  *http.Client
	urls  scm.URLBuilder
	clock func() time.Time
}

// New returns a Client backed by the given HTTP client. The HTTP client is
// expected to inject BasicAuth and any retry logic (see internal/httpx).
// A nil http.Client falls back to http.DefaultClient – mainly useful for
// tests; production code should always supply a configured client.
func New(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		cfg:  cfg,
		http: httpClient,
		urls: scm.URLBuilder{
			ClientBase:    cfg.ClientBase,
			InClusterBase: cfg.InClusterBase,
			NamePrefix:    cfg.NamePrefix,
		},
		clock: time.Now,
	}
}

// Name returns the provider identifier.
func (c *Client) Name() string { return "scm-manager" }

// GitOpsUsername returns the configured technical user.
func (c *Client) GitOpsUsername() string { return c.cfg.GitOpsUsername }

// RepoURL returns the in-cluster or client-facing URL for namespace/name.
func (c *Client) RepoURL(namespace, name string, scope scm.RepoURLScope) string {
	switch scope {
	case scm.RepoURLClient:
		return c.urls.ClientRepoURL(namespace, name)
	default:
		return c.urls.InClusterRepoURL(namespace, name)
	}
}

// --- DTOs -----------------------------------------------------------------

// repository is the request body of POST /v2/repositories/.
//
// We marshal explicitly with `omitempty` semantics that Groovy got "for
// free" because Jackson skips null values.
type repository struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Contact     string `json:"contact,omitempty"`
}

// permission is the request body of POST
// /v2/repositories/{ns}/{name}/permissions/.
type permission struct {
	Name            string   `json:"name"`
	Role            string   `json:"role"`
	GroupPermission bool     `json:"groupPermission"`
	Verbs           []string `json:"verbs"`
}

// user is the request body of POST /v2/users.
type user struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName"`
	Mail        string                 `json:"mail"`
	External    bool                   `json:"external"`
	Password    string                 `json:"password"`
	Active      bool                   `json:"active"`
	Links       map[string]interface{} `json:"_links"`
}

// permissionCollection is the request body of PUT /v2/users/{u}/permissions.
type permissionCollection struct {
	Permissions []string `json:"permissions"`
}

// --- Public API ------------------------------------------------------------

// CreateRepository implements scm.Provider.
func (c *Client) CreateRepository(ctx context.Context, namespace, name, description string, init bool) (bool, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("scm-manager: namespace and name must both be non-empty")
	}
	body := repository{
		Namespace:   namespace,
		Name:        name,
		Type:        "git",
		Description: description,
	}
	path := "v2/repositories/"
	if init {
		path += "?initialize=true"
	} else {
		path += "?initialize=false"
	}
	resp, err := c.do(ctx, http.MethodPost, path, "application/vnd.scmm-repository+json;v=2", body)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	created, err := handle201or409(resp, fmt.Sprintf("Repository %s/%s", namespace, name))
	return created, err
}

// SetRepositoryPermission implements scm.Provider.
func (c *Client) SetRepositoryPermission(ctx context.Context, namespace, name, principal string, role scm.Role, scope scm.Scope) error {
	if strings.TrimSpace(principal) == "" {
		return fmt.Errorf("scm-manager: principal must not be empty")
	}
	body := permission{
		Name:            principal,
		Role:            mapRole(role),
		GroupPermission: scope == scm.ScopeGroup,
		Verbs:           []string{},
	}
	path := fmt.Sprintf("v2/repositories/%s/%s/permissions/", url.PathEscape(namespace), url.PathEscape(name))
	resp, err := c.do(ctx, http.MethodPost, path, "application/vnd.scmm-repositoryPermission+json", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = handle201or409(resp, fmt.Sprintf("Permission on %s/%s", namespace, name))
	return err
}

// EnsureUser implements scm.Provider. The mail argument defaults to
// "changeme@test.local" if empty (matching ScmManagerSetup.addUser).
func (c *Client) EnsureUser(ctx context.Context, login, password, display, mail string) error {
	if strings.TrimSpace(login) == "" {
		return fmt.Errorf("scm-manager: user login must not be empty")
	}
	if display == "" {
		display = login
	}
	if mail == "" {
		mail = "changeme@test.local"
	}
	body := user{
		Name:        login,
		DisplayName: display,
		Mail:        mail,
		External:    false,
		Password:    password,
		Active:      true,
		Links:       map[string]interface{}{},
	}
	resp, err := c.do(ctx, http.MethodPost, "v2/users", "application/vnd.scmm-user+json;v=2", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = handle201or409(resp, fmt.Sprintf("User %s", login))
	return err
}

// EnsureNamespace is a no-op for SCM-Manager: namespaces materialise
// automatically when the first repository is created. We only validate
// the input so callers get a clear error early.
func (c *Client) EnsureNamespace(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("scm-manager: namespace must not be empty")
	}
	return nil
}

// SetUserPermissions sets the SCM-Manager *global* permission collection
// for the given user (e.g. ["metrics:read"]). This corresponds to
// UsersApi.setPermissionForUser in the Groovy code.
func (c *Client) SetUserPermissions(ctx context.Context, login string, perms []string) error {
	if strings.TrimSpace(login) == "" {
		return fmt.Errorf("scm-manager: user login must not be empty")
	}
	body := permissionCollection{Permissions: perms}
	path := fmt.Sprintf("v2/users/%s/permissions", url.PathEscape(login))
	resp, err := c.do(ctx, http.MethodPut, path, "application/vnd.scmm-permissionCollection+json;v=2", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return expectStatus(resp, fmt.Sprintf("permissions for %s", login), http.StatusOK, http.StatusNoContent)
}

// InstallPlugin posts to /v2/plugins/available/{name}/install. SCM-Manager
// answers 200/204 on success and 409 if the plugin is already installed.
// We treat both as success – this matches handleApiResponse() in the
// Groovy code.
func (c *Client) InstallPlugin(ctx context.Context, pluginName string, restart bool) error {
	if strings.TrimSpace(pluginName) == "" {
		return fmt.Errorf("scm-manager: plugin name must not be empty")
	}
	q := ""
	if restart {
		q = "?restart=true"
	} else {
		q = "?restart=false"
	}
	path := fmt.Sprintf("v2/plugins/available/%s/install%s", url.PathEscape(pluginName), q)
	resp, err := c.do(ctx, http.MethodPost, path, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return expectStatus(resp, fmt.Sprintf("install plugin %s", pluginName),
		http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusCreated, http.StatusConflict)
}

// SetConfig pushes the v2/config object (used by ScmManagerSetup).
func (c *Client) SetConfig(ctx context.Context, cfg map[string]interface{}) error {
	resp, err := c.do(ctx, http.MethodPut, "v2/config", "application/vnd.scmm-config+json;v=2", cfg)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return expectStatus(resp, "scmm config", http.StatusOK, http.StatusNoContent)
}

// ConfigureJenkinsPlugin sends the per-plugin Jenkins configuration.
func (c *Client) ConfigureJenkinsPlugin(ctx context.Context, cfg map[string]interface{}) error {
	resp, err := c.do(ctx, http.MethodPut, "v2/config/jenkins/", "application/json", cfg)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return expectStatus(resp, "jenkins plugin config", http.StatusOK, http.StatusNoContent)
}

// CheckAvailable performs a GET v2 and returns nil on a 2xx response.
func (c *Client) CheckAvailable(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "v2", "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("scm-manager: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
}

// WaitUntilAvailable polls CheckAvailable until either a 2xx is observed,
// the context is cancelled, or the overall timeout elapses. The bug fix
// vs. the Groovy implementation is the ctx.Done() select – the original
// busy-waited via Thread.sleep and could only be stopped by hand.
func (c *Client) WaitUntilAvailable(ctx context.Context, timeout, interval time.Duration) error {
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		if err := c.CheckAvailable(deadlineCtx); err == nil {
			return nil
		}
		select {
		case <-deadlineCtx.Done():
			if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("scm-manager: not available after %s", timeout)
			}
			return deadlineCtx.Err()
		case <-time.After(interval):
		}
	}
}

// --- HTTP helpers ---------------------------------------------------------

func (c *Client) do(ctx context.Context, method, path, contentType string, body interface{}) (*http.Response, error) {
	full, err := c.absoluteURL(path)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("scm-manager: marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, reader)
	if err != nil {
		return nil, fmt.Errorf("scm-manager: build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scm-manager: %s %s: %w", method, full, err)
	}
	return resp, nil
}

// absoluteURL prefixes path with the configured APIBase. The base is
// expected to end in /api/ but we handle the other variants too.
func (c *Client) absoluteURL(path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(c.cfg.APIBase), "/")
	if base == "" {
		return "", errors.New("scm-manager: APIBase is not configured")
	}
	return base + "/" + strings.TrimLeft(path, "/"), nil
}

// handle201or409 returns true on 201 (freshly created), false on 409 (already
// existed) and an error on any other status code. The error includes the
// HTTP status text and the response body for easier debugging.
func handle201or409(resp *http.Response, what string) (bool, error) {
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		return true, nil
	case http.StatusConflict:
		return false, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return false, fmt.Errorf("scm-manager: could not create %s: HTTP %d %s: %s",
			what, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(body)))
	}
}

// expectStatus consumes the body and returns nil iff the response status
// is in the allowed list.
func expectStatus(resp *http.Response, what string, allowed ...int) error {
	for _, ok := range allowed {
		if resp.StatusCode == ok {
			_, _ = io.Copy(io.Discard, resp.Body)
			return nil
		}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	return fmt.Errorf("scm-manager: %s: HTTP %d %s: %s",
		what, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(body)))
}

// mapRole maps the provider-agnostic scm.Role to the SCM-Manager
// vocabulary. MAINTAIN is downgraded to WRITE (matching the Groovy
// implementation) and ADMIN is upgraded to OWNER.
func mapRole(r scm.Role) string {
	switch r {
	case scm.RoleRead:
		return "READ"
	case scm.RoleWrite, scm.RoleMaintain:
		return "WRITE"
	case scm.RoleAdmin, scm.RoleOwner:
		return "OWNER"
	default:
		return "READ"
	}
}

// Compile-time assertion that Client satisfies scm.Provider.
var _ scm.Provider = (*Client)(nil)
