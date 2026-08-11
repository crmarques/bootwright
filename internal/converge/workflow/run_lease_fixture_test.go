package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

func saveRunLeaseFixture(runsDir string, lease RunLease) error {
	return withRunLeaseLock(runsDir, func() error {
		data, err := runLeaseBytes(lease)
		if err != nil {
			return err
		}
		if err := safefs.WriteFileEnsuringDir(LeasePath(runsDir), data, 0o600); err != nil {
			return fmt.Errorf("write apply lease fixture: %w", err)
		}
		return nil
	})
}
