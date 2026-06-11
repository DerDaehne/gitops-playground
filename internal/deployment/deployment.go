// Package deployment is the Go counterpart of the
// infrastructure/deployment/* classes from the Groovy source. It picks the
// right deployment strategy (helm-imperatively vs Argo CD application) and
// renders helm-values templates the same way Tool.deployHelmChart does.
package deployment

import (
	"context"
	"fmt"
)

// RepoType matches DeploymentStrategy.RepoType.
type RepoType int

const (
	RepoHelm RepoType = iota
	RepoGit
)

// Spec describes a single feature deployment.
type Spec struct {
	RepoURL     string
	RepoName    string
	ChartOrPath string
	Version     string
	Namespace   string
	ReleaseName string
	// HelmValuesPath is the path to a rendered, on-disk values.yaml.
	// Use RenderHelmValues to build it before calling Deploy.
	HelmValuesPath string
	RepoType       RepoType
}

// Strategy deploys one Spec.
type Strategy interface {
	Deploy(ctx context.Context, s Spec) error
}

// Deployer chooses between Argo CD and Helm depending on whether Argo CD
// is active. Mirrors infrastructure/deployment/Deployer.groovy.
type Deployer struct {
	ArgoCD Strategy
	Helm   Strategy
	// ArgoCDActive returns true when the ArgoCD feature is enabled. The
	// runner sets this from the active Config so deploys honor the user
	// choice even after re-merges.
	ArgoCDActive func() bool
}

// Deploy implements Strategy.
func (d Deployer) Deploy(ctx context.Context, s Spec) error {
	if d.ArgoCDActive != nil && d.ArgoCDActive() {
		if d.ArgoCD == nil {
			return fmt.Errorf("deployment: ArgoCD strategy is required but not configured")
		}
		return d.ArgoCD.Deploy(ctx, s)
	}
	if d.Helm == nil {
		return fmt.Errorf("deployment: Helm strategy is required but not configured")
	}
	return d.Helm.Deploy(ctx, s)
}
