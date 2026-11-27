package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace/catalogpurge"
)

func TestCatalogPurgePresenterShowsTheForceImpactBeforeConfirmation(t *testing.T) {
	plan := catalogpurge.Plan{Kind: catalogpurge.Media, Force: true, Entries: []catalogpurge.Entry{
		{Name: "installer.iso", ReservedBy: []string{"lab"}},
	}}
	var out bytes.Buffer
	present := NewCatalogPurgePresenter(&out)
	if err := present(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Catalog purge plan", "would delete", "Force includes", "Reserved by", "lab"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("preview omits %q: %s", want, out.String())
		}
	}
	out.Reset()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := present(canceled, plan); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("canceled preview wrote %q or returned %v", out.String(), err)
	}
	if err := NewCatalogPurgePresenter(nil)(t.Context(), plan); err == nil {
		t.Fatal("a confirmation could proceed with no preview writer")
	}
	report := presentCatalogPurge(catalogpurge.Result{Plan: catalogpurge.Plan{Kind: catalogpurge.Media}})
	if err := writeCatalogPurge(&out, report); err != nil || !strings.Contains(out.String(), "No registered entries.") {
		t.Fatalf("empty result = %s, %v", out.String(), err)
	}
}

func TestStructuredPurgeRequiresDryRunOrYes(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		var out, errOut bytes.Buffer
		runner := New(Config{Out: &out, ErrOut: &errOut})
		code := runner.Run(t.Context(), []string{"catalog", "media", "purge", "--force", "--output", format})
		if code != 2 || !strings.Contains(out.String()+errOut.String(), "purge requires --dry-run or --yes") {
			t.Fatalf("%s force without confirmation: %d %s %s", format, code, out.String(), errOut.String())
		}
	}
}

func TestCatalogPurgeConfirmationNamesTheCatalogAndPropagatesRefusals(t *testing.T) {
	for _, kind := range []catalogpurge.Kind{catalogpurge.Artifact, catalogpurge.Media} {
		ask := confirmationOf("purge catalog "+string(kind), "2 entries")
		if !strings.Contains(ask.prompt, "2 entries from the "+string(kind)+" catalog") || ask.review != "the purge plan" {
			t.Fatalf("unexpected prompt: %+v", ask)
		}
	}
	refusal := catalogpurge.Failure(catalogpurge.Artifact, "the catalog changed", "inspect the fresh purge plan")
	var out, errOut bytes.Buffer
	record := &dispatchRecord{err: refusal}
	runner := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)})
	code := runner.Run(t.Context(), []string{"catalog", "artifact", "purge", "--yes"})
	if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "the catalog changed") {
		t.Fatalf("purge refusal = %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}
