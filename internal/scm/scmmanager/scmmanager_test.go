package scmmanager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// newServer spins up an httptest.Server with the given handler and returns
// a Client pointed at it.
func newServer(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := Config{
		APIBase:        srv.URL + "/scm/api/",
		ClientBase:     srv.URL + "/scm",
		InClusterBase:  srv.URL + "/scm",
		NamePrefix:     "",
		GitOpsUsername: "gitops",
	}
	return New(cfg, srv.Client()), srv
}

func TestCreateRepository_Created(t *testing.T) {
	var gotBody repository
	var gotPath string
	var gotQuery string
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		if ct := r.Header.Get("Content-Type"); ct != "application/vnd.scmm-repository+json;v=2" {
			t.Errorf("unexpected Content-Type %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	})

	created, err := c.CreateRepository(context.Background(), "ns", "name", "desc", true)
	if err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if gotPath != "/scm/api/v2/repositories/" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "initialize=true" {
		t.Errorf("query = %q", gotQuery)
	}
	if gotBody.Namespace != "ns" || gotBody.Name != "name" || gotBody.Type != "git" || gotBody.Description != "desc" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestCreateRepository_Conflict(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	created, err := c.CreateRepository(context.Background(), "ns", "n", "", false)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if created {
		t.Fatalf("expected created=false on 409")
	}
}

func TestSetRepositoryPermission(t *testing.T) {
	var gotPath string
	var gotBody permission
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if ct := r.Header.Get("Content-Type"); ct != "application/vnd.scmm-repositoryPermission+json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	})

	err := c.SetRepositoryPermission(context.Background(), "tenant", "repo", "ops-team", scm.RoleWrite, scm.ScopeGroup)
	if err != nil {
		t.Fatalf("SetRepositoryPermission: %v", err)
	}
	if gotPath != "/scm/api/v2/repositories/tenant/repo/permissions/" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.Name != "ops-team" || gotBody.Role != "WRITE" || !gotBody.GroupPermission {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestSetRepositoryPermission_MaintainDowngradesToWrite(t *testing.T) {
	var gotBody permission
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	})
	if err := c.SetRepositoryPermission(context.Background(), "ns", "n", "alice", scm.RoleMaintain, scm.ScopeUser); err != nil {
		t.Fatal(err)
	}
	if gotBody.Role != "WRITE" {
		t.Errorf("MAINTAIN should map to WRITE, got %q", gotBody.Role)
	}
	if gotBody.GroupPermission {
		t.Errorf("ScopeUser should not be a group permission")
	}
}

func TestEnsureUser(t *testing.T) {
	var gotPath string
	var gotBody user
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if ct := r.Header.Get("Content-Type"); ct != "application/vnd.scmm-user+json;v=2" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	})

	if err := c.EnsureUser(context.Background(), "alice", "s3cret", "", ""); err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if gotPath != "/scm/api/v2/users" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.Name != "alice" || gotBody.DisplayName != "alice" || gotBody.Password != "s3cret" ||
		gotBody.Mail != "changeme@test.local" || gotBody.External || !gotBody.Active {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestEnsureUser_Conflict(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	if err := c.EnsureUser(context.Background(), "alice", "x", "Alice", "a@example.com"); err != nil {
		t.Fatalf("conflict should be swallowed, got %v", err)
	}
}

func TestEnsureNamespace(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("EnsureNamespace must not perform any HTTP call, got %s %s", r.Method, r.URL.Path)
	})
	if err := c.EnsureNamespace(context.Background(), "tenant"); err != nil {
		t.Fatalf("EnsureNamespace: %v", err)
	}
	if err := c.EnsureNamespace(context.Background(), "  "); err == nil {
		t.Fatalf("empty namespace should error")
	}
}

func TestRepoURL(t *testing.T) {
	c := &Client{cfg: Config{
		ClientBase:    "http://client.example/scm",
		InClusterBase: "http://scmm.svc/scm",
	}, urls: scm.URLBuilder{
		ClientBase:    "http://client.example/scm",
		InClusterBase: "http://scmm.svc/scm",
	}}
	if got := c.RepoURL("ns", "name", scm.RepoURLInCluster); got != "http://scmm.svc/scm/repo/ns/name" {
		t.Errorf("in-cluster URL = %q", got)
	}
	if got := c.RepoURL("ns", "name", scm.RepoURLClient); got != "http://client.example/scm/repo/ns/name" {
		t.Errorf("client URL = %q", got)
	}
}

func TestErrorContainsBody(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	})
	_, err := c.CreateRepository(context.Background(), "ns", "n", "", false)
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error mentioning status+body, got %v", err)
	}
}

func TestWaitUntilAvailable_RespectsContextCancellation(t *testing.T) {
	var calls int32
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.WaitUntilAvailable(ctx, 5*time.Second, 20*time.Millisecond)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Errorf("expected error after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitUntilAvailable did not return after cancel")
	}
	if atomic.LoadInt32(&calls) == 0 {
		t.Error("expected at least one poll attempt")
	}
}

// Ensure the Client satisfies the Provider interface at compile time even
// in test builds.
var _ scm.Provider = (*Client)(nil)
