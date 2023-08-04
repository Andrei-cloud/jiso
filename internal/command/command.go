package command

type Command interface {
	Name() string
	Synopsis() string
	Execute() error
}
