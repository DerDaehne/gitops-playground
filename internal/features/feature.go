package features

type Feature interface {
	Name() string
	IsEnabled() bool
	Validate() error
	Install() error
}
