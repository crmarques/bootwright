//go:build !linux

package output

import "os"

func terminalWidth(_ *os.File) int { return 0 }
