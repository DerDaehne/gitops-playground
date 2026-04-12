package helm

import "helm.sh/helm/v3/pkg/chartutil"

// HelmConfig represents all data needed to run an helm deployment
type HelmConfig struct {
	Chart   string
	RepoUrl string
	Version string
	Values  chartutil.Values
}

// Client is a high-level wrapper around the Helm SDK action layer.
// TODO: promote standalone functions to methods on Client.
type Client struct{}
