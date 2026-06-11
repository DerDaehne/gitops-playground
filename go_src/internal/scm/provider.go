// Package scm defines the SCM provider abstraction used by gitops-playground.
//
// It is the Go counterpart of
// infrastructure/git/providers/GitProvider.groovy. Only the operations
// actually exercised by the Go port are exposed:
//
//   - CreateRepository / SetRepositoryPermission – needed by the
//     application bootstrap and the content loader.
//   - EnsureUser / EnsureNamespace – needed for SCM-Manager setup and the
//     gitops/metrics technical accounts. For GitLab "namespace" maps to a
//     subgroup under the configured parent group, "user" is a no-op
//     because the Groovy code never created GitLab users either.
//   - RepoURL / GitOpsUsername – read-only helpers used elsewhere in the
//     port.
//
// Deliberately omitted (compared to the Groovy interface):
//
//   - DeleteRepository / DeleteUser / SetDefaultBranch – the Groovy
//     implementations are explicit no-ops on both providers, see
//     ScmManager.groovy lines 132-153 and Gitlab.groovy lines 139-158.
//   - Prometheus endpoint helpers and Credentials accessors – consumed by
//     other Go packages directly from the config, not via this interface.
package scm

import (
	"context"
	"fmt"
	"strings"
)

// Provider is the minimal contract every SCM backend must satisfy.
type Provider interface {
	// Name returns a short identifier (e.g. "scm-manager", "gitlab"). It
	// is used for log lines and error messages, never for routing.
	Name() string

	// CreateRepository creates the repo identified by namespace/name. If
	// init is true the backend should initialise the repository with an
	// initial commit (a README for GitLab, the SCM-Manager "initialize"
	// query parameter for SCM-Manager).
	//
	// Returns (true, nil) on a freshly created repository, (false, nil)
	// when the repository already existed (HTTP 409 / project lookup
	// success), and a non-nil error on every other failure.
	CreateRepository(ctx context.Context, namespace, name, description string, init bool) (created bool, err error)

	// SetRepositoryPermission grants principal (user or group) the given
	// role on namespace/name. scope distinguishes user-permissions from
	// group-permissions.
	SetRepositoryPermission(ctx context.Context, namespace, name, principal string, role Role, scope Scope) error

	// EnsureUser makes sure a user with the given login exists. Calling
	// it again for an existing user must be a no-op (HTTP 409 swallowed).
	EnsureUser(ctx context.Context, login, password, display, mail string) error

	// EnsureNamespace makes sure the given namespace exists. On
	// SCM-Manager namespaces are created implicitly when repositories are
	// created, so this method only needs to verify the namespace is not
	// empty; on GitLab it creates a subgroup under the configured parent
	// group.
	EnsureNamespace(ctx context.Context, name string) error

	// RepoURL returns the canonical URL for namespace/name in the given
	// scope. Callers use this for git clone/push URLs, ArgoCD repo
	// secrets and similar places.
	RepoURL(namespace, name string, scope RepoURLScope) string

	// GitOpsUsername returns the technical username used by ArgoCD /
	// Jenkins to authenticate against the SCM.
	GitOpsUsername() string
}

// Role is the provider-agnostic permission level.
type Role int8

// Role values mirror AccessRole from GitProvider.groovy. We deliberately
// keep only the ones actually used by gitops-playground.
const (
	RoleRead Role = iota
	RoleWrite
	RoleMaintain
	RoleAdmin
	RoleOwner
)

// String returns the role's name, useful for log lines.
func (r Role) String() string {
	switch r {
	case RoleRead:
		return "READ"
	case RoleWrite:
		return "WRITE"
	case RoleMaintain:
		return "MAINTAIN"
	case RoleAdmin:
		return "ADMIN"
	case RoleOwner:
		return "OWNER"
	default:
		return fmt.Sprintf("Role(%d)", r)
	}
}

// Scope distinguishes user from group principals.
type Scope int8

const (
	ScopeUser Scope = iota
	ScopeGroup
)

// String returns the scope's name.
func (s Scope) String() string {
	switch s {
	case ScopeUser:
		return "USER"
	case ScopeGroup:
		return "GROUP"
	default:
		return fmt.Sprintf("Scope(%d)", s)
	}
}

// RepoURLScope chooses between the in-cluster and the client-facing URL
// flavour. The constants mirror the original Groovy enum.
type RepoURLScope int8

const (
	// RepoURLInCluster is for workloads running inside Kubernetes (ArgoCD,
	// Jobs, in-cluster automation).
	RepoURLInCluster RepoURLScope = iota
	// RepoURLClient is for interactive or CI clients performing
	// push/clone. If the application itself runs inside Kubernetes the
	// Service DNS is used; otherwise a NodePort or external base.
	RepoURLClient
)

// String returns a human-readable representation.
func (s RepoURLScope) String() string {
	switch s {
	case RepoURLInCluster:
		return "IN_CLUSTER"
	case RepoURLClient:
		return "CLIENT"
	default:
		return fmt.Sprintf("RepoURLScope(%d)", s)
	}
}

// SplitRepoTarget splits a "namespace/name" string into its two parts.
// Both pieces are trimmed. An error is returned when the input does not
// contain exactly one slash – the Groovy code happily indexed into
// `repoTarget.split('/', 2)[1]` which crashes with a less helpful
// ArrayIndexOutOfBoundsException.
func SplitRepoTarget(repoTarget string) (namespace, name string, err error) {
	t := strings.TrimSpace(repoTarget)
	idx := strings.Index(t, "/")
	if idx <= 0 || idx == len(t)-1 {
		return "", "", fmt.Errorf("scm: repo target %q must have the form 'namespace/name'", repoTarget)
	}
	return strings.TrimSpace(t[:idx]), strings.TrimSpace(t[idx+1:]), nil
}
