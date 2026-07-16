//go:build linux

package safefs

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func UnmountAllUnder(dir string) error {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read mount table: %w", err)
	}
	defer file.Close()
	mounts, err := mountpointsUnder(file, dir)
	if err != nil {
		return err
	}
	for _, mount := range mounts {
		if unix.Unmount(mount, 0) == nil {
			continue
		}
		err := unix.Unmount(mount, unix.MNT_DETACH)
		if err == nil || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
			continue
		}
		return fmt.Errorf("unmount %s: %w", mount, err)
	}
	return nil
}
