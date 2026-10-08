//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

// A media row that failed because a non-regular entry occupies the image's or
// its record's name is removed by media delete without following it; a
// directory that still holds entries is refused naming it and its remedy, and
// nothing inside it is removed.
func TestAFailedNonRegularMediaEntryIsRemovedByMediaDelete(t *testing.T) {
	ctx := context.Background()
	replace := func(path string, occupy func(string) error) error {
		if err := os.Remove(path); err != nil {
			return err
		}
		return occupy(path)
	}
	for name, row := range map[string]struct {
		entry  string
		occupy func(path, outside string) error
	}{
		"image empty directory":  {"demo.iso", func(path, _ string) error { return os.Mkdir(path, 0700) }},
		"image fifo":             {"demo.iso", func(path, _ string) error { return syscall.Mkfifo(path, 0600) }},
		"image symlink":          {"demo.iso", func(path, outside string) error { return os.Symlink(outside, path) }},
		"record empty directory": {"demo.iso.json", func(path, _ string) error { return os.Mkdir(path, 0700) }},
	} {
		t.Run(name, func(t *testing.T) {
			store := mediaFixture(t)
			addMedia(t, store, "demo.iso", "installer bytes", false)
			outside := filepath.Join(filepath.Dir(store.options.Root), "outside.iso")
			if err := os.WriteFile(outside, []byte("outside bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := replace(filepath.Join(store.options.Root, "media", row.entry), func(path string) error { return row.occupy(path, outside) }); err != nil {
				t.Fatal(err)
			}
			listed, err := mediaService(store, &mediaAcquirer{}).List(ctx, media.ListMediaRequest{})
			if err != nil || len(listed.Media) != 1 || listed.Media[0].Verified != "failed" {
				t.Fatalf("the damaged row was listed as %+v (%#v)", listed, diagnostics.Of(err))
			}
			if _, err := media.New(store, &mediaAcquirer{}, &mediaPrompt{store: store}, mediaClock{}).Delete(ctx, media.DeleteMediaRequest{Name: "demo.iso"}); err != nil {
				t.Fatalf("delete: %#v", diagnostics.Of(err))
			}
			if entries := mediaDirectory(t, store); len(entries) != 0 {
				t.Fatalf("media directory = %v", entries)
			}
			if kept, err := os.ReadFile(outside); err != nil || string(kept) != "outside bytes" {
				t.Fatalf("the symlink target changed: %q (%v)", kept, err)
			}
		})
	}
	t.Run("image directory holding entries", func(t *testing.T) {
		store := mediaFixture(t)
		addMedia(t, store, "demo.iso", "installer bytes", false)
		image := filepath.Join(store.options.Root, "media", "demo.iso")
		if err := replace(image, func(path string) error { return os.Mkdir(path, 0700) }); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(image, "kept"), []byte("kept"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := media.New(store, &mediaAcquirer{}, &mediaPrompt{store: store}, mediaClock{}).Delete(ctx, media.DeleteMediaRequest{Name: "demo.iso"})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "media.store" ||
			reported[0].Message != "media entry media/demo.iso is a directory that still holds entries, which media delete never removes" ||
			reported[0].Remediation != "remove media/demo.iso beneath the state root, then repeat bootwright media delete --name demo.iso" {
			t.Fatalf("refusal = %#v", reported)
		}
		if _, err := os.Stat(filepath.Join(image, "kept")); err != nil {
			t.Fatalf("an entry inside the directory was removed (%v)", err)
		}
	})
}
