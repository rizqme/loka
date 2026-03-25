//go:build darwin

package vm

func NewManager(name string) (VMManager, error) {
	return NewVZManager(name), nil
}
