package api

import "fmt"

type Repository struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Type        string `json:"type"`
	Contact     string `json:"contact"`
	Description string `json:"description"`
}

type Permission struct {
	Name            string         `json:"name"`
	Role            PermissionRole `json:"role"`
	GroupPermission bool           `json:"groupPermission"`
}

type PermissionRole string

const (
	PermissionRoleRead  PermissionRole = "READ"
	PermissionRoleWrite PermissionRole = "WRITE"
	PermissionRoleOwner PermissionRole = "OWNER"
)

func (c *ScmManagerApiClient) CreateRepository(repo Repository, initialize bool) error {
	initStr := "false"
	if initialize {
		initStr = "true"
	}
	path := fmt.Sprintf("v2/repositories/?initialize=%s", initStr)
	resp, err := c.doJSON("POST", path, repo, "application/vnd.scmm-repository+json;v=2")
	if err != nil {
		return err
	}
	return handle201or409(resp, fmt.Sprintf("Repository %s/%s", repo.Namespace, repo.Name))
}

func (c *ScmManagerApiClient) CreatePermission(namespace string, name string, perm Permission) error {
	path := fmt.Sprintf("v2/repositories/%s/%s/permissions/", namespace, name)
	resp, err := c.doJSON("POST", path, perm, "application/vnd.scmm-repositoryPermission+json")
	if err != nil {
		return err
	}
	return handle201or409(resp, fmt.Sprintf("Permission on %s/%s", namespace, name))
}
