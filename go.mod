module github.com/cloudogu/gitops-playground/go

go 1.22

require (
	github.com/go-git/go-git/v5 v5.12.0
	github.com/spf13/cobra v1.8.1
	github.com/spf13/pflag v1.0.5
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.31.1
	k8s.io/apimachinery v0.31.1
	k8s.io/client-go v0.31.1
	sigs.k8s.io/yaml v1.4.0
)

// All transitive (indirect) dependencies pulled in by go-git/client-go
// will be added automatically the first time `go mod tidy` runs in
// this module. They are deliberately omitted here so we don't pin
// hashes we cannot verify without network access.

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
)
