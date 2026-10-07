package selectionfs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// An unsafe selection object is refused naming it and its repair, so an
// operator never has to find which of the two the account owns wrongly.
func TestAnUnsafeSelectionObjectIsNamedWithItsRepair(t *testing.T) {
	const (
		file          = "selection file ~/.bootwright/context has an unsafe owner, type, link count or mode"
		fileRepair    = "remove it and select again with bootwright context use --name <context>"
		directory     = "~/.bootwright has an unsafe owner, type or mode"
		directoryFix  = "make ~/.bootwright a directory you own with mode 0700"
		selectionName = "lab"
	)
	for name, row := range map[string]struct {
		damage               func(home string) error
		message, remediation string
	}{
		"marker mode": {func(home string) error { return os.Chmod(filepath.Join(home, ".bootwright", "context"), 0644) }, file, fileRepair},
		"marker link count": {func(home string) error {
			return os.Link(filepath.Join(home, ".bootwright", "context"), filepath.Join(home, "second"))
		}, file, fileRepair},
		"marker type": {func(home string) error {
			path := filepath.Join(home, ".bootwright", "context")
			if err := os.Remove(path); err != nil {
				return err
			}
			return syscall.Mkfifo(path, 0600)
		}, file, fileRepair},
		"marker symlink": {func(home string) error {
			path := filepath.Join(home, ".bootwright", "context")
			if err := os.Rename(path, filepath.Join(home, "elsewhere")); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(home, "elsewhere"), path)
		}, file, fileRepair},
		"marker unreadable": {func(home string) error { return os.Chmod(filepath.Join(home, ".bootwright", "context"), 0) }, file, fileRepair},
		"directory mode":    {func(home string) error { return os.Chmod(filepath.Join(home, ".bootwright"), 0755) }, directory, directoryFix},
		"directory symlink": {func(home string) error {
			path := filepath.Join(home, ".bootwright")
			if err := os.Rename(path, filepath.Join(home, "elsewhere")); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(home, "elsewhere"), path)
		}, directory, directoryFix},
		"directory type": {func(home string) error {
			path := filepath.Join(home, ".bootwright")
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			return os.WriteFile(path, nil, 0600)
		}, directory, directoryFix},
	} {
		t.Run(name, func(t *testing.T) {
			store, home := fixture(t)
			value := contexts.Selection{Version: contexts.SelectionVersion, Name: selectionName}
			if err := store.Write(context.Background(), value); err != nil {
				t.Fatal(err)
			}
			if err := row.damage(home); err != nil {
				t.Fatal(err)
			}
			_, readErr := store.Read(context.Background())
			for _, err := range []error{readErr, store.Write(context.Background(), value)} {
				reported := diagnostics.Of(err)
				if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != row.message || reported[0].Remediation != row.remediation {
					t.Fatalf("got %#v, want %q with repair %q", reported, row.message, row.remediation)
				}
			}
		})
	}
}
