package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// fakeGitLab is a tiny in-memory model of the few GitLab REST endpoints
// our Client touches. It is enough to drive the happy-path tests of the
// scm.Provider methods.
type fakeGitLab struct {
	t              *testing.T
	parent         group
	subgroups      map[string]*group // path -> subgroup, scoped to parent.ID
	projectsByPath map[string]*project
	users          map[string]*apiUser
	groups         map[string]*group
	nextID         int64
	createdShares  []shareGroupRequest
	createdMembers []addMemberRequest
	expectToken    string
}

func (f *fakeGitLab) handler(w http.ResponseWriter, r *http.Request) {
	if f.expectToken != "" && r.Header.Get("PRIVATE-TOKEN") != f.expectToken {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	switch {
	// Parent group lookup
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/groups/parent":
		writeJSON(w, http.StatusOK, f.parent)
	// Subgroups list
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/groups/"+itoa(f.parent.ID)+"/subgroups":
		out := make([]group, 0, len(f.subgroups))
		for _, g := range f.subgroups {
			out = append(out, *g)
		}
		writeJSON(w, http.StatusOK, out)
	// Direct projects list (used by ensureSubgroup collision check)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/groups/"+itoa(f.parent.ID)+"/projects":
		writeJSON(w, http.StatusOK, []project{})
	// Project lookup by full path
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v4/projects/"):
		path := strings.TrimPrefix(r.URL.Path, "/api/v4/projects/")
		// path is URL-encoded
		decoded := mustUnescape(path)
		if p, ok := f.projectsByPath[decoded]; ok {
			writeJSON(w, http.StatusOK, p)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	// Create group
	case r.Method == http.MethodPost && r.URL.Path == "/api/v4/groups":
		var body createGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.nextID++
		g := &group{ID: f.nextID, Name: body.Name, Path: body.Path, FullPath: f.parent.FullPath + "/" + body.Path, ParentID: body.ParentID}
		f.subgroups[body.Path] = g
		writeJSON(w, http.StatusCreated, g)
	// Create project
	case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects":
		var body createProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.nextID++
		// Find the subgroup for this namespace id.
		var nsPath string
		for _, g := range f.subgroups {
			if g.ID == body.NamespaceID {
				nsPath = g.Path
			}
		}
		fullPath := f.parent.FullPath + "/" + nsPath + "/" + body.Path
		p := &project{ID: f.nextID, Name: body.Name, Path: body.Path, NamespaceID: body.NamespaceID, PathWithNamespace: fullPath}
		f.projectsByPath[fullPath] = p
		writeJSON(w, http.StatusCreated, p)
	// Search groups
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/groups":
		q := r.URL.Query().Get("search")
		out := make([]group, 0)
		if g, ok := f.groups[q]; ok {
			out = append(out, *g)
		}
		writeJSON(w, http.StatusOK, out)
	// Search users
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/users":
		q := r.URL.Query().Get("search")
		out := make([]apiUser, 0)
		if u, ok := f.users[q]; ok {
			out = append(out, *u)
		}
		writeJSON(w, http.StatusOK, out)
	// Share project with group
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/share"):
		var body shareGroupRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.createdShares = append(f.createdShares, body)
		w.WriteHeader(http.StatusCreated)
	// Add project member
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/members"):
		var body addMemberRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.createdMembers = append(f.createdMembers, body)
		w.WriteHeader(http.StatusCreated)
	default:
		f.t.Logf("unhandled request: %s %s", r.Method, r.URL.String())
		http.Error(w, "unhandled", http.StatusNotImplemented)
	}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func itoa(i int64) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	out := ""
	for i > 0 {
		out = string(digits[i%10]) + out
		i /= 10
	}
	if negative {
		out = "-" + out
	}
	return out
}

func mustUnescape(p string) string {
	// httptest decodes the path before handing it to the handler, but
	// callers escape "tenant/ns/repo" as "tenant%2Fns%2Frepo" which then
	// shows up here un-escaped. We keep the helper as a single seam in
	// case GitLab's encoding ever differs.
	return p
}

func newFakeClient(t *testing.T) (*Client, *fakeGitLab, *httptest.Server) {
	t.Helper()
	fake := &fakeGitLab{
		t:              t,
		parent:         group{ID: 42, Name: "parent", Path: "parent", FullPath: "tenant/parent"},
		subgroups:      map[string]*group{},
		projectsByPath: map[string]*project{},
		users:          map[string]*apiUser{},
		groups:         map[string]*group{},
		nextID:         100,
		expectToken:    "tok",
	}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	cfg := Config{
		BaseURL:           srv.URL,
		Token:             "tok",
		ParentGroup:       "parent",
		DefaultVisibility: "private",
		GitOpsUsername:    "gitops",
	}
	return New(cfg, srv.Client()), fake, srv
}

func TestCreateRepository_CreatesSubgroupAndProject(t *testing.T) {
	c, fake, _ := newFakeClient(t)
	created, err := c.CreateRepository(context.Background(), "Apps", "Demo", "demo project", true)
	if err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if _, ok := fake.subgroups["apps"]; !ok {
		t.Errorf("expected subgroup 'apps' to be created, have %v", fake.subgroups)
	}
	if _, ok := fake.projectsByPath["tenant/parent/apps/demo"]; !ok {
		t.Errorf("expected project at tenant/parent/apps/demo, have %v", fake.projectsByPath)
	}
}

func TestCreateRepository_ProjectAlreadyExists(t *testing.T) {
	c, fake, _ := newFakeClient(t)
	fake.subgroups["apps"] = &group{ID: 7, Name: "apps", Path: "apps", FullPath: "tenant/parent/apps", ParentID: fake.parent.ID}
	fake.projectsByPath["tenant/parent/apps/demo"] = &project{ID: 8, Name: "Demo", Path: "demo", NamespaceID: 7, PathWithNamespace: "tenant/parent/apps/demo"}

	created, err := c.CreateRepository(context.Background(), "apps", "demo", "", true)
	if err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}
	if created {
		t.Fatalf("expected created=false on existing project")
	}
}

func TestSetRepositoryPermission_AddsMember(t *testing.T) {
	c, fake, _ := newFakeClient(t)
	fake.subgroups["apps"] = &group{ID: 7, Name: "apps", Path: "apps", FullPath: "tenant/parent/apps", ParentID: fake.parent.ID}
	fake.projectsByPath["tenant/parent/apps/demo"] = &project{ID: 8, NamespaceID: 7, Path: "demo", PathWithNamespace: "tenant/parent/apps/demo"}
	fake.users["alice"] = &apiUser{ID: 11, Username: "alice", Email: "a@example.com"}

	if err := c.SetRepositoryPermission(context.Background(), "apps", "demo", "alice", scm.RoleWrite, scm.ScopeUser); err != nil {
		t.Fatalf("SetRepositoryPermission: %v", err)
	}
	if len(fake.createdMembers) != 1 {
		t.Fatalf("expected one add-member call, got %d", len(fake.createdMembers))
	}
	if fake.createdMembers[0].UserID != 11 {
		t.Errorf("unexpected member: %+v", fake.createdMembers[0])
	}
	if fake.createdMembers[0].AccessLevel != 30 {
		t.Errorf("expected Developer (30), got %d", fake.createdMembers[0].AccessLevel)
	}
}

func TestSetRepositoryPermission_SharesWithGroup(t *testing.T) {
	c, fake, _ := newFakeClient(t)
	fake.subgroups["apps"] = &group{ID: 7, Name: "apps", Path: "apps", FullPath: "tenant/parent/apps", ParentID: fake.parent.ID}
	fake.projectsByPath["tenant/parent/apps/demo"] = &project{ID: 8, NamespaceID: 7, Path: "demo", PathWithNamespace: "tenant/parent/apps/demo"}
	fake.groups["devs"] = &group{ID: 21, Name: "devs", Path: "devs", FullPath: "devs"}

	if err := c.SetRepositoryPermission(context.Background(), "apps", "demo", "devs", scm.RoleOwner, scm.ScopeGroup); err != nil {
		t.Fatalf("SetRepositoryPermission: %v", err)
	}
	if len(fake.createdShares) != 1 {
		t.Fatalf("expected one share call, got %d", len(fake.createdShares))
	}
	if fake.createdShares[0].GroupID != 21 || fake.createdShares[0].GroupAccess != 50 {
		t.Errorf("unexpected share: %+v", fake.createdShares[0])
	}
}

func TestEnsureUser_NoOp(t *testing.T) {
	c, _, _ := newFakeClient(t)
	if err := c.EnsureUser(context.Background(), "alice", "pw", "Alice", "a@example.com"); err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if err := c.EnsureUser(context.Background(), "  ", "pw", "Alice", "a@example.com"); err == nil {
		t.Fatalf("expected error for empty login")
	}
}

func TestEnsureNamespace_CreatesSubgroup(t *testing.T) {
	c, fake, _ := newFakeClient(t)
	if err := c.EnsureNamespace(context.Background(), "Apps"); err != nil {
		t.Fatalf("EnsureNamespace: %v", err)
	}
	if _, ok := fake.subgroups["apps"]; !ok {
		t.Errorf("expected subgroup 'apps' to be created, have %v", fake.subgroups)
	}
}

func TestEnsureNamespace_Idempotent(t *testing.T) {
	c, fake, _ := newFakeClient(t)
	fake.subgroups["apps"] = &group{ID: 7, Name: "apps", Path: "apps", FullPath: "tenant/parent/apps", ParentID: fake.parent.ID}
	if err := c.EnsureNamespace(context.Background(), "apps"); err != nil {
		t.Fatalf("EnsureNamespace: %v", err)
	}
	if len(fake.subgroups) != 1 {
		t.Errorf("expected no new subgroup, have %v", fake.subgroups)
	}
}

func TestMapAccessLevel(t *testing.T) {
	cases := []struct {
		role  scm.Role
		scope scm.Scope
		want  int
	}{
		{scm.RoleRead, scm.ScopeUser, 20},
		{scm.RoleWrite, scm.ScopeUser, 30},
		{scm.RoleMaintain, scm.ScopeUser, 40},
		{scm.RoleAdmin, scm.ScopeUser, 40},
		{scm.RoleOwner, scm.ScopeGroup, 50},
		{scm.RoleOwner, scm.ScopeUser, 40},
	}
	for _, tc := range cases {
		if got := mapAccessLevel(tc.role, tc.scope); got != tc.want {
			t.Errorf("mapAccessLevel(%s,%s)=%d, want %d", tc.role, tc.scope, got, tc.want)
		}
	}
}

// Compile-time assertion.
var _ scm.Provider = (*Client)(nil)
