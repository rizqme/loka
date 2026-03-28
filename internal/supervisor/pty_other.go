//go:build !linux

package supervisor

import (
	"fmt"
	"os"
)

var errPTYNotSupported = fmt.Errorf("PTY allocation is only supported on Linux")

func openPTY() (master *os.File, slavePath string, err error) {
	return nil, "", errPTYNotSupported
}

func setPTYSize(master *os.File, rows, cols uint16) error {
	return errPTYNotSupported
}
