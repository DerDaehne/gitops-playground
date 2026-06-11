package cli

// ReturnCode mirrors com.cloudogu.gitops.cli.ReturnCode.
type ReturnCode int

const (
	ReturnSuccess      ReturnCode = 0
	ReturnGenericError ReturnCode = 1
	ReturnNotConfirmed ReturnCode = 2
)
