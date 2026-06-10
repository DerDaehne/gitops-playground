package cli

// Version metadata. Set at link time via -ldflags "-X ...".
var (
	AppName    = "gitops-playground (GOP)"
	BinaryName = "apply-ng"
	Version    = "dev"
	Commit     = ""
)

// VersionString returns the user-facing version banner.
func VersionString() string {
	if Commit != "" {
		return AppName + " " + Version + " (" + Commit + ")"
	}
	return AppName + " " + Version
}
