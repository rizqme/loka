//go:build linux

package supervisor

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// openPTY allocates a new pseudo-terminal pair.
// Returns the master file and the slave device path (e.g. /dev/pts/3).
func openPTY() (master *os.File, slavePath string, err error) {
	// Try /dev/ptmx first, fall back to /dev/pts/ptmx (available when devpts
	// is mounted with -o ptmxmode=666 but devtmpfs doesn't provide /dev/ptmx).
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master, err = os.OpenFile("/dev/pts/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
		if err != nil {
			return nil, "", fmt.Errorf("open /dev/ptmx: %w (also tried /dev/pts/ptmx)", err)
		}
	}

	// Get slave PTY number.
	var ptyno uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&ptyno))); errno != 0 {
		master.Close()
		return nil, "", fmt.Errorf("TIOCGPTN: %w", errno)
	}
	slavePath = fmt.Sprintf("/dev/pts/%d", ptyno)

	// Unlock slave.
	var unlock int32 = 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		return nil, "", fmt.Errorf("TIOCSPTLCK: %w", errno)
	}

	return master, slavePath, nil
}

// winsize matches the kernel struct winsize for TIOCSWINSZ.
type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// setPTYSize sets the terminal dimensions on the PTY master.
func setPTYSize(master *os.File, rows, cols uint16) error {
	ws := winsize{Row: rows, Col: cols}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		return fmt.Errorf("TIOCSWINSZ: %w", errno)
	}
	return nil
}
