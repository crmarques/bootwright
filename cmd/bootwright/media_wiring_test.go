package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

// mediaShelf is a media store holding one published, unreserved image: enough
// for the assembled media service to verify it and to delete it.
type mediaShelf struct {
	entry   managedos.MediaEntry
	deleted bool
}

func (s *mediaShelf) ReadMedia(_ context.Context, callback func(media.View) error) error {
	return callback(s)
}

func (s *mediaShelf) MutateMedia(_ context.Context, callback func(media.Transaction) error) error {
	return callback(s)
}

func (s *mediaShelf) Entries(context.Context) ([]media.Image, error) {
	if s.deleted {
		return []media.Image{}, nil
	}
	return []media.Image{{MediaEntry: s.entry, Observed: s.entry.Size}}, nil
}

func (s *mediaShelf) Names(context.Context) ([]string, error) {
	if s.deleted {
		return []string{}, nil
	}
	return []string{s.entry.Name}, nil
}

func (s *mediaShelf) Digest(context.Context, string) (string, error) { return s.entry.SHA256, nil }

func (s *mediaShelf) Reservations(context.Context) (map[string][]string, error) {
	return map[string][]string{}, nil
}

func (s *mediaShelf) Entry(_ context.Context, name string) (managedos.MediaEntry, bool, error) {
	if s.deleted || name != s.entry.Name {
		return managedos.MediaEntry{}, false, nil
	}
	return s.entry, true, nil
}

func (s *mediaShelf) Retained(context.Context) ([]managedos.MediaEntry, error) {
	return []managedos.MediaEntry{}, nil
}

func (s *mediaShelf) Stage(context.Context, string, string) (media.Stage, error) {
	return nil, errors.New("this store stages nothing")
}

func (s *mediaShelf) Publish(context.Context, string, media.Stage, []byte, bool) error {
	return errors.New("this store publishes nothing")
}

func (s *mediaShelf) Delete(context.Context, string) error {
	s.deleted = true
	return nil
}

// acceptingMediaPrompt accepts every confirmation and counts them.
type acceptingMediaPrompt struct{ prompts int }

func (p *acceptingMediaPrompt) Confirm(context.Context, string, string) error {
	p.prompts++
	return nil
}

// TestTheShippedMediaServiceReportsAndPresentsOnStdout holds the shipped graph
// to F-015 and F-078: an interactive process builds the media presenter, the
// local dependencies bind it beside the invocation's progress, and the
// assembled media service applies both, so a checksum's check row and the
// image a deletion confirms reach the invocation's standard output.
func TestTheShippedMediaServiceReportsAndPresentsOnStdout(t *testing.T) {
	ctx := context.Background()
	args := []string{"media", "list", "--checksums"}
	var out, errOut bytes.Buffer
	process, hooks := interactiveProcess(cli.ClassifyInvocation(args), args, &out, &errOut, controller.Route{})
	defer hooks.finish()
	if process.MediaPresenter == nil {
		t.Fatal("an interactive process built no media presenter")
	}
	local, release := localServiceDependencies(process)
	defer release()
	if local.Media.Presenter != process.MediaPresenter {
		t.Fatalf("the local media dependencies bound %#v as the presenter, want the process's %#v", local.Media.Presenter, process.MediaPresenter)
	}
	sum := sha256.Sum256([]byte("data"))
	shelf := &mediaShelf{entry: managedos.MediaEntry{
		Name: "demo.iso", Size: 4, SHA256: hex.EncodeToString(sum[:]), Source: "file:///images/demo.iso", Added: "2026-09-15T09:00:00Z",
	}}
	prompt := &acceptingMediaPrompt{}
	local.Media.Store, local.Media.Confirmer = shelf, prompt
	services := assembleServices(local)

	listed, err := services.Media.List(ctx, media.ListMediaRequest{Checksums: true})
	if err != nil || len(listed.Media) != 1 || listed.Media[0].Verified != "ok" {
		t.Fatalf("the shipped media list = %+v (%#v)", listed, diagnostics.Of(err))
	}
	checks := "\nChecks\n  [RUNNING]  [1/1] Verify demo.iso\n  [OK]       [1/1] Verify demo.iso: matches its record"
	if !strings.HasPrefix(out.String(), checks) {
		t.Fatalf("the shipped media list reported %q, want it to begin %q", out.String(), checks)
	}

	out.Reset()
	if _, err := services.Media.Delete(ctx, media.DeleteMediaRequest{Name: "demo.iso"}); err != nil || !shelf.deleted || prompt.prompts != 1 {
		t.Fatalf("the shipped media delete: deleted %t, prompts %d (%#v)", shelf.deleted, prompt.prompts, diagnostics.Of(err))
	}
	shown := "Media deletion\n\n  Name    demo.iso\n  Size    4\n  Digest  sha256:" + shelf.entry.SHA256 +
		"\n  Added   2026-09-15T09:00:00Z\n  Source  file:///images/demo.iso\n"
	if out.String() != shown {
		t.Fatalf("the shipped media delete showed %q, want %q", out.String(), shown)
	}
	if errOut.Len() != 0 {
		t.Fatalf("the shipped media service wrote %q to standard error", errOut.String())
	}
}
