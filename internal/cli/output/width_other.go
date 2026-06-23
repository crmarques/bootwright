//go:build !linux

package output

import "os"

// terminalWidth has no portable implementation off Linux; returning 0 disables
// wrap accounting in RenderFrame (one row per logical line).
func terminalWidth(_ *os.File) int { return 0 }
