// Package gitlab is the Go counterpart of
// infrastructure/git/providers/gitlab/Gitlab.groovy.
//
// The Groovy implementation talks to GitLab through gitlab4j-api. We
// replace that with hand-written calls against the documented REST API
// (https://docs.gitlab.com/ee/api/). Only the surface that the
// gitops-playground actually exercises is implemented:
//
//   - Subgroup discovery / creation under a configured parent group.
//   - Project discovery / creation under such a subgroup.
//   - Project membership (single user) and project share (group).
//
// Bug fixes vs. the Groovy original:
//
//   - parentFullPath() in Gitlab.groovy fetches the parent group twice per
//     CreateRepository call (parentGroup() + parentFullPath()) – we cache
//     the lookup per Client.
//   - resolveFullPath() concatenates parentGroupId verbatim with the repo
//     target, which is wrong whenever parentGroupId is a numeric ID (the
//     URL ends up like "42/ns/repo"). We use the parent's fullPath.
//   - findDirectSubgroupByPath relied on a paginated endpoint without
//     paging through it. We page through all subgroups so the lookup is
//     correct on instances with many groups.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// Config is the subset of GitlabConfig we need.
type Config struct {
	// BaseURL is the GitLab instance root, e.g. "https://gitlab.example.com".
	BaseURL string
	// Token is the PAT used as Authorization header (PRIVATE-TOKEN).
	// Empty when callers prefer to inject auth via the HTTP client.
	Token string
	// ParentGroup is either a numeric group ID or the full path of the
	// group under which all repositories are created.
	ParentGroup string
	// ParentFullPath is the resolved full path of ParentGroup, set
	// once at wire-time so that scm.Provider.RepoURL — which has no
	// ctx — does not need a sync HTTP round trip. When empty, RepoURL
	// falls back to a context.Background() resolveParent(). Tracked
	// as REMAINING T-4: prefer setting this at wire time.
	ParentFullPath string
	// DefaultVisibility, one of "public", "internal", "private". Empty
	// defaults to "private".
	DefaultVisibility string
	// GitOpsUsername is the technical user used by ArgoCD / Jenkins.
	GitOpsUsername string
	// NamePrefix is the optional tenant prefix used by RepoURL helpers.
	NamePrefix string
}

// Client talks to a single GitLab instance.
type Client struct {
	cfg  Config
	http *http.Client

	parentOnce sync.Once
	parent     *group
	parentErr  error
}

// New returns a Client backed by the given HTTP client. The http client
// must inject the Authorization or PRIVATE-TOKEN header on its own if
// cfg.Token is empty. If both are configured, the explicit Token in
// Config wins (PRIVATE-TOKEN header).
func New(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{cfg: cfg, http: httpClient}
}

// Name implements scm.Provider.
func (c *Client) Name() string { return "gitlab" }

// GitOpsUsername implements scm.Provider.
func (c *Client) GitOpsUsername() string { return c.cfg.GitOpsUsername }

// RepoURL returns the project clone URL. GitLab does not distinguish
// in-cluster vs. client URLs (it serves both off the same host), so the
// scope is ignored – matching the Groovy behaviour in Gitlab.repoUrl.
//
// If cfg.ParentFullPath is preset (preferred wire-time path), no HTTP
// call happens. Otherwise the fallback resolveParent is invoked with a
// background context — see the doc on Config.ParentFullPath.
func (c *Client) RepoURL(namespace, name string, _ scm.RepoURLScope) string {
	parentPath := c.cfg.ParentFullPath
	if parentPath == "" {
		parentPath, _ = c.parentFullPath(context.Background())
	}
	return scm.GitLabRepoURL(c.cfg.BaseURL, parentPath, strings.ToLower(namespace), strings.ToLower(name))
}

// ResolveParentFullPath is meant to be called once at wire time so that
// later RepoURL calls (which have no ctx) hit the cache. It populates
// the same internal cache resolveParent uses and returns the resolved
// FullPath.
func (c *Client) ResolveParentFullPath(ctx context.Context) (string, error) {
	if c.cfg.ParentFullPath != "" {
		return c.cfg.ParentFullPath, nil
	}
	return c.parentFullPath(ctx)
}

// --- DTOs -----------------------------------------------------------------

type group struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	FullPath string `json:"full_path"`
	ParentID int64  `json:"parent_id"`
}

type project struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"path_with_namespace"`
	NamespaceID       int64  `json:"namespace_id"`
}

type apiUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type createProjectRequest struct {
	Name                 string `json:"name"`
	Path                 string `json:"path"`
	NamespaceID          int64  `json:"namespace_id"`
	Description          string `json:"description,omitempty"`
	Visibility           string `json:"visibility,omitempty"`
	InitializeWithReadme bool   `json:"initialize_with_readme"`
	IssuesEnabled        bool   `json:"issues_enabled"`
	MergeRequestsEnabled bool   `json:"merge_requests_enabled"`
	WikiEnabled          bool   `json:"wiki_enabled"`
	SnippetsEnabled      bool   `json:"snippets_enabled"`
}

type createGroupRequest struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	ParentID int64  `json:"parent_id,omitempty"`
}

type addMemberRequest struct {
	UserID      int64 `json:"user_id"`
	AccessLevel int   `json:"access_level"`
}

type shareGroupRequest struct {
	GroupID     int64 `json:"group_id"`
	GroupAccess int   `json:"group_access"`
}

// --- Public API ------------------------------------------------------------

// CreateRepository implements scm.Provider.
func (c *Client) CreateRepository(ctx context.Context, namespace, name, description string, init bool) (bool, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("gitlab: namespace and name must both be non-empty")
	}
	parent, err := c.resolveParent(ctx)
	if err != nil {
		return false, err
	}

	nsPath := strings.ToLower(namespace)
	projectPath := strings.ToLower(name)
	subgroupID, err := c.ensureSubgroup(ctx, parent, nsPath)
	if err != nil {
		return false, err
	}

	fullPath := fmt.Sprintf("%s/%s/%s", parent.FullPath, nsPath, projectPath)
	existing, err := c.findProject(ctx, fullPath)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil
	}

	body := createProjectRequest{
		Name:                 name,
		Path:                 projectPath,
		NamespaceID:          subgroupID,
		Description:          description,
		Visibility:           visibilityOrDefault(c.cfg.DefaultVisibility),
		InitializeWithReadme: init,
		IssuesEnabled:        false,
		MergeRequestsEnabled: false,
		WikiEnabled:          false,
		SnippetsEnabled:      false,
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v4/projects", body)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return false, nil
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return false, httpErr(resp, "create project")
	}
	return true, nil
}

// SetRepositoryPermission implements scm.Provider.
func (c *Client) SetRepositoryPermission(ctx context.Context, namespace, name, principal string, role scm.Role, scope scm.Scope) error {
	parent, err := c.resolveParent(ctx)
	if err != nil {
		return err
	}
	fullPath := fmt.Sprintf("%s/%s/%s", parent.FullPath, strings.ToLower(namespace), strings.ToLower(name))
	p, err := c.findProject(ctx, fullPath)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("gitlab: project %q not found", fullPath)
	}
	level := mapAccessLevel(role, scope)

	switch scope {
	case scm.ScopeGroup:
		grp, err := c.findGroup(ctx, principal)
		if err != nil {
			return err
		}
		if grp == nil {
			return fmt.Errorf("gitlab: group %q not found", principal)
		}
		resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/share", p.ID),
			shareGroupRequest{GroupID: grp.ID, GroupAccess: level})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return httpErr(resp, "share project with group")
		}
	case scm.ScopeUser:
		u, err := c.findUser(ctx, principal)
		if err != nil {
			return err
		}
		if u == nil {
			return fmt.Errorf("gitlab: user %q not found", principal)
		}
		resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/members", p.ID),
			addMemberRequest{UserID: u.ID, AccessLevel: level})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
			return httpErr(resp, "add project member")
		}
	default:
		return fmt.Errorf("gitlab: unknown scope %s", scope)
	}
	return nil
}

// EnsureUser is a no-op on GitLab – the Groovy implementation does not
// create users either; gitops-playground assumes the gitops/metrics user
// already exists when GitLab is the SCM backend.
func (c *Client) EnsureUser(_ context.Context, login, _, _, _ string) error {
	if strings.TrimSpace(login) == "" {
		return fmt.Errorf("gitlab: user login must not be empty")
	}
	return nil
}

// EnsureNamespace creates a subgroup under the configured parent group
// if it does not exist yet. It corresponds to ensureSubgroupUnderParentId
// in the Groovy code but is split out so namespaces can be created
// independently of repositories.
func (c *Client) EnsureNamespace(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("gitlab: namespace must not be empty")
	}
	parent, err := c.resolveParent(ctx)
	if err != nil {
		return err
	}
	_, err = c.ensureSubgroup(ctx, parent, strings.ToLower(name))
	return err
}

// --- helpers --------------------------------------------------------------

func (c *Client) resolveParent(ctx context.Context) (*group, error) {
	c.parentOnce.Do(func() {
		raw := strings.TrimSpace(c.cfg.ParentGroup)
		if raw == "" {
			c.parentErr = errors.New("gitlab: parent group id is required")
			return
		}
		// Allow both numeric IDs and full paths.
		identifier := raw
		if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
			identifier = strings.TrimLeft(raw, "/")
		}
		resp, err := c.do(ctx, http.MethodGet, "/api/v4/groups/"+url.PathEscape(identifier), nil)
		if err != nil {
			c.parentErr = err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			c.parentErr = httpErr(resp, "lookup parent group")
			return
		}
		var g group
		if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
			c.parentErr = fmt.Errorf("gitlab: decode parent group: %w", err)
			return
		}
		c.parent = &g
	})
	return c.parent, c.parentErr
}

func (c *Client) parentFullPath(ctx context.Context) (string, error) {
	p, err := c.resolveParent(ctx)
	if err != nil || p == nil {
		return "", err
	}
	return p.FullPath, nil
}

// ensureSubgroup makes sure 'segPath' exists as a direct subgroup of
// 'parent' and returns its ID.
func (c *Client) ensureSubgroup(ctx context.Context, parent *group, segPath string) (int64, error) {
	existing, err := c.findDirectSubgroupByPath(ctx, parent.ID, segPath)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return existing.ID, nil
	}
	// Guard against a project with the same path already living under
	// the parent group.
	if collision, err := c.findDirectProjectByPath(ctx, parent.ID, segPath); err != nil {
		return 0, err
	} else if collision != nil {
		return 0, fmt.Errorf("gitlab: cannot create subgroup %q under %q: a project with that path already exists",
			segPath, parent.FullPath)
	}
	body := createGroupRequest{Name: segPath, Path: segPath, ParentID: parent.ID}
	resp, err := c.do(ctx, http.MethodPost, "/api/v4/groups", body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		var g group
		if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
			return 0, fmt.Errorf("gitlab: decode created group: %w", err)
		}
		return g.ID, nil
	case http.StatusBadRequest, http.StatusConflict:
		// Treat as a race – re-fetch.
		retry, rerr := c.findDirectSubgroupByPath(ctx, parent.ID, segPath)
		if rerr != nil {
			return 0, rerr
		}
		if retry != nil {
			return retry.ID, nil
		}
		return 0, httpErr(resp, "create subgroup")
	default:
		return 0, httpErr(resp, "create subgroup")
	}
}

// findDirectSubgroupByPath pages through /groups/{id}/subgroups until it
// finds segPath. Pagination uses GitLab's X-Next-Page header.
func (c *Client) findDirectSubgroupByPath(ctx context.Context, parentID int64, segPath string) (*group, error) {
	page := 1
	for {
		path := fmt.Sprintf("/api/v4/groups/%d/subgroups?per_page=100&page=%d", parentID, page)
		resp, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			err := httpErr(resp, "list subgroups")
			resp.Body.Close()
			return nil, err
		}
		var groups []group
		if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("gitlab: decode subgroups: %w", err)
		}
		next := resp.Header.Get("X-Next-Page")
		resp.Body.Close()
		for i := range groups {
			if groups[i].Path == segPath {
				return &groups[i], nil
			}
		}
		if next == "" {
			return nil, nil
		}
		n, err := strconv.Atoi(next)
		if err != nil || n <= page {
			return nil, nil
		}
		page = n
	}
}

func (c *Client) findDirectProjectByPath(ctx context.Context, parentID int64, segPath string) (*project, error) {
	page := 1
	for {
		path := fmt.Sprintf("/api/v4/groups/%d/projects?per_page=100&page=%d", parentID, page)
		resp, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			err := httpErr(resp, "list projects")
			resp.Body.Close()
			return nil, err
		}
		var projects []project
		if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("gitlab: decode projects: %w", err)
		}
		next := resp.Header.Get("X-Next-Page")
		resp.Body.Close()
		for i := range projects {
			if projects[i].Path == segPath {
				return &projects[i], nil
			}
		}
		if next == "" {
			return nil, nil
		}
		n, err := strconv.Atoi(next)
		if err != nil || n <= page {
			return nil, nil
		}
		page = n
	}
}

func (c *Client) findProject(ctx context.Context, fullPath string) (*project, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v4/projects/"+url.PathEscape(fullPath), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpErr(resp, "lookup project")
	}
	var p project
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("gitlab: decode project: %w", err)
	}
	return &p, nil
}

func (c *Client) findGroup(ctx context.Context, search string) (*group, error) {
	resp, err := c.do(ctx, http.MethodGet,
		"/api/v4/groups?search="+url.QueryEscape(search)+"&per_page=100", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpErr(resp, "search groups")
	}
	var groups []group
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return nil, fmt.Errorf("gitlab: decode groups: %w", err)
	}
	for i := range groups {
		if groups[i].FullPath == search || groups[i].Path == search || groups[i].Name == search {
			return &groups[i], nil
		}
	}
	return nil, nil
}

func (c *Client) findUser(ctx context.Context, search string) (*apiUser, error) {
	resp, err := c.do(ctx, http.MethodGet,
		"/api/v4/users?search="+url.QueryEscape(search)+"&per_page=100", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpErr(resp, "search users")
	}
	var users []apiUser
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, fmt.Errorf("gitlab: decode users: %w", err)
	}
	for i := range users {
		if users[i].Username == search || users[i].Email == search {
			return &users[i], nil
		}
	}
	return nil, nil
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	base := strings.TrimRight(strings.TrimSpace(c.cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("gitlab: BaseURL is not configured")
	}
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("gitlab: marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("gitlab: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.cfg.Token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.cfg.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: %s %s: %w", method, base+path, err)
	}
	return resp, nil
}

func httpErr(resp *http.Response, what string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	return fmt.Errorf("gitlab: %s: HTTP %d %s: %s",
		what, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(body)))
}

func visibilityOrDefault(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "public":
		return "public"
	case "internal":
		return "internal"
	case "", "private":
		return "private"
	default:
		return "private"
	}
}

// mapAccessLevel translates the provider-agnostic role to GitLab's
// numeric access levels (https://docs.gitlab.com/ee/api/access_requests.html).
func mapAccessLevel(role scm.Role, scope scm.Scope) int {
	switch role {
	case scm.RoleRead:
		return 20 // Reporter (Guests cannot read code on private projects)
	case scm.RoleWrite:
		return 30 // Developer
	case scm.RoleMaintain:
		return 40 // Maintainer
	case scm.RoleAdmin:
		return 40 // No project-level admin – cap to Maintainer.
	case scm.RoleOwner:
		if scope == scm.ScopeGroup {
			return 50 // Owner
		}
		return 40 // For project members the cap is Maintainer.
	default:
		return 20
	}
}

// Compile-time assertion.
var _ scm.Provider = (*Client)(nil)
