//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// exampleMediaRecords answers the records a host media store holding every
// image the shipped examples name would publish, so their plans freeze fixed
// sizes and digests. without names one image the store does not hold.
type exampleMediaRecords struct{ without string }

func (r exampleMediaRecords) MediaRecords(context.Context) (map[string]installation.MediaRecord, error) {
	records := map[string]installation.MediaRecord{
		"rhel-9.8-x86_64-boot.iso": {SHA256: strings.Repeat("0123456789abcdef", 4), Size: 1105199104, Observed: 1105199104},
		"rhel-9.8-x86_64-dvd.iso":  {SHA256: strings.Repeat("fedcba9876543210", 4), Size: 13107200000, Observed: 13107200000},
	}
	delete(records, r.without)
	return records, nil
}

// plannedInstallations is every installation request one example's plan
// freezes, with its images pinned at the example media records.
func plannedInstallations(t *testing.T, name, controllerMachine, contextName string) []installation.Request {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, name))
	plan, err := installation.New(nil).WithMedia(exampleMediaRecords{}).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: controllerMachine,
		Context: lifecycle.ContextIdentity{Name: contextName},
	})
	if err != nil {
		t.Fatalf("planning %s: %v", name, diagnostics.Of(err))
	}
	requests := make([]installation.Request, 0, len(plan.Definitions))
	for _, definition := range plan.Definitions {
		request, err := installation.DecodeRequest(definition.Request)
		if err != nil {
			t.Fatalf("decoding %s: %v", definition.ID, diagnostics.Of(err))
		}
		requests = append(requests, request)
	}
	return requests
}

// An image the host store does not hold refuses the plan, which every
// registration follows, naming the image and the command that imports it.
func TestLabRHELExamplePlanRefusesAnImageMissingFromTheStore(t *testing.T) {
	state, _ := compileAcceptance(t, labExampleSources(t))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{without: "rhel-9.8-x86_64-dvd.iso"})
	capability, ok := resolver.Resolve(installation.Kind, installation.Implementation)
	if !ok {
		t.Fatal("the installation capability does not resolve")
	}
	_, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: "lab-rhel"},
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || !strings.Contains(reported[0].Message, "holds no image rhel-9.8-x86_64-dvd.iso") ||
		!strings.Contains(reported[0].Remediation, "bootwright media add --name rhel-9.8-x86_64-dvd.iso ") {
		t.Fatalf("diagnostics = %+v (%v)", reported, err)
	}
}

// failedShelf lists the boot image and an image the store cannot read, as the
// context store lists an image whose record is unreadable or undecodable.
type failedShelf struct{ *mediaShelf }

func (s failedShelf) ReadMedia(_ context.Context, callback func(media.View) error) error {
	return callback(s)
}

func (s failedShelf) Entries(context.Context) ([]media.Image, error) {
	return []media.Image{
		{MediaEntry: s.entry, Observed: s.entry.Size},
		{MediaEntry: managedos.MediaEntry{Name: "rhel-9.8-x86_64-dvd.iso"}, Failure: "its record cannot be decoded"},
	}, nil
}

// An image the store lists as failed refuses the plan, naming the store's
// cause, instead of freezing an empty digest an apply would fail on later.
func TestLabRHELExamplePlanRefusesAnImageTheStoreCannotRead(t *testing.T) {
	state, _ := compileAcceptance(t, labExampleSources(t))
	shelf := failedShelf{&mediaShelf{entry: managedos.MediaEntry{Name: "rhel-9.8-x86_64-boot.iso", Size: 9, SHA256: strings.Repeat("a", 64)}}}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), storeMediaRecords{store: shelf})
	capability, ok := resolver.Resolve(installation.Kind, installation.Implementation)
	if !ok {
		t.Fatal("the installation capability does not resolve")
	}
	_, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "controller",
		Context: lifecycle.ContextIdentity{Name: "lab-rhel"},
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || !strings.Contains(reported[0].Message, "cannot read its image rhel-9.8-x86_64-dvd.iso") ||
		!strings.HasSuffix(reported[0].Message, ": its record cannot be decoded") ||
		!strings.Contains(reported[0].Remediation, "bootwright media delete --name rhel-9.8-x86_64-dvd.iso") {
		t.Fatalf("diagnostics = %+v (%v)", reported, err)
	}
}

// The shipped graph plans installations through the context store's own media
// records, and a graph wired without a store plans none.
func TestProductionLifecycleWiringReadsTheMediaStore(t *testing.T) {
	deps, release := localServiceDependencies(processDependencies{})
	defer release()
	if deps.Lifecycle.Media == nil || any(deps.Lifecycle.Media) != any(deps.Repository) {
		t.Fatalf("production wired %#v as the lifecycle's media store", deps.Lifecycle.Media)
	}
	if reader, ok := lifecycleMedia(deps.Lifecycle).(storeMediaRecords); !ok || reader.store != deps.Lifecycle.Media {
		t.Fatalf("the installation capability reads %#v", lifecycleMedia(deps.Lifecycle))
	}
	if reader := lifecycleMedia(lifecycleDependencies{}); reader != nil {
		t.Fatalf("a lifecycle without a store reads %#v", reader)
	}
	shelf := &mediaShelf{entry: managedos.MediaEntry{Name: "rhel-9.8-x86_64-boot.iso", Size: 9, SHA256: strings.Repeat("a", 64)}}
	records, err := storeMediaRecords{store: shelf}.MediaRecords(context.Background())
	want := installation.MediaRecord{SHA256: strings.Repeat("a", 64), Size: 9, Observed: 9}
	if err != nil || len(records) != 1 || records["rhel-9.8-x86_64-boot.iso"] != want {
		t.Fatalf("records = %+v (%v)", records, err)
	}
}

// A plan reads the media records while its lifecycle read holds the shared
// root lock, which the store's shared read is compatible with; an attempt runs
// under the exclusive lock, where the same read is refused as busy, so only
// the plan reads the store and the attempt proves the bytes itself.
func TestPlanningReadsTheMediaStoreUnderTheSharedLock(t *testing.T) {
	services, repository, input, _ := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	reader := storeMediaRecords{store: repository}
	err := repository.ReadLifecycle(context.Background(), "alpha", func(lifecycle.View) error {
		records, err := reader.MediaRecords(context.Background())
		if err == nil && len(records) != 0 {
			t.Errorf("an empty store answered %+v", records)
		}
		return err
	})
	if err != nil {
		t.Fatalf("reading the media store under the shared lock: %v", diagnostics.Of(err))
	}
	err = repository.MutateLifecycle(context.Background(), "alpha", func(lifecycle.Transaction) error {
		_, err := reader.MediaRecords(context.Background())
		return err
	})
	if !errors.Is(err, media.ErrBusy) {
		t.Fatalf("reading the media store under the exclusive lock = %v, want busy", err)
	}
}
