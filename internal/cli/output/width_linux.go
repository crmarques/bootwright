//go:build linux

package output

import (
	"os"

	"golang.org/x/sys/unix"
)

// terminalWidth returns the column count of the terminal backing w, or 0 when
// it cannot be determined (not a TTY, or the ioctl fails). A 0 result disables
// wrap accounting in RenderFrame, which is safe: a non-TTY never redraws.
func terminalWidth(w *os.File) int {
	ws, err := unix.IoctlGetWinsize(int(w.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil {
		return 0
	}
	return int(ws.Col)
}
