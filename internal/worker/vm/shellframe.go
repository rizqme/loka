package vm

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Shell frame types for the vsock PTY tunnel protocol.
// After the shell_start RPC response, the vsock connection uses framed messages:
//
//	[1 byte type][2 bytes payload length, big-endian][payload]
const (
	FrameData   byte = 0x01 // PTY data (stdin or stdout).
	FrameResize byte = 0x02 // Terminal resize (4 bytes: rows u16 + cols u16 BE).
	FrameExit   byte = 0x03 // Shell exited (4 bytes: exit code i32 BE).
)

// ShellFrame is a single message in the shell framing protocol.
type ShellFrame struct {
	Type byte
	Data []byte
}

// WriteFrame writes a framed message to w.
func WriteFrame(w io.Writer, f ShellFrame) error {
	hdr := [3]byte{f.Type, 0, 0}
	binary.BigEndian.PutUint16(hdr[1:], uint16(len(f.Data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(f.Data) > 0 {
		_, err := w.Write(f.Data)
		return err
	}
	return nil
}

// ReadFrame reads a framed message from r.
func ReadFrame(r io.Reader) (ShellFrame, error) {
	var hdr [3]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return ShellFrame{}, err
	}
	length := binary.BigEndian.Uint16(hdr[1:])
	data := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, data); err != nil {
			return ShellFrame{}, err
		}
	}
	return ShellFrame{Type: hdr[0], Data: data}, nil
}

// ResizePayload encodes terminal dimensions into a resize frame payload.
func ResizePayload(rows, cols uint16) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint16(buf[0:2], rows)
	binary.BigEndian.PutUint16(buf[2:4], cols)
	return buf
}

// ParseResize decodes terminal dimensions from a resize frame payload.
func ParseResize(data []byte) (rows, cols uint16, err error) {
	if len(data) < 4 {
		return 0, 0, fmt.Errorf("resize payload too short: %d bytes", len(data))
	}
	rows = binary.BigEndian.Uint16(data[0:2])
	cols = binary.BigEndian.Uint16(data[2:4])
	return rows, cols, nil
}

// ExitPayload encodes an exit code into an exit frame payload.
func ExitPayload(code int) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(code))
	return buf
}

// ParseExit decodes an exit code from an exit frame payload.
func ParseExit(data []byte) int {
	if len(data) < 4 {
		return -1
	}
	return int(int32(binary.BigEndian.Uint32(data)))
}
