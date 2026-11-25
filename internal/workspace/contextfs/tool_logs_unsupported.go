//go:build !linux || !amd64

package contextfs

import "github.com/crmarques/bootwright/internal/reconciliation/logview"

func OpenNativeToolLog(string) (logview.LogFile, string, error) {
	return nil, "", nil
}
