//go:build linux

package output

import (
	"os"

	"golang.org/x/sys/unix"
)

func terminalWidth(w *os.File) int {
	ws, err := unix.IoctlGetWinsize(int(w.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil {
		return 0
	}
	return int(ws.Col)
}
