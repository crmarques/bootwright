package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

type fakeSecretReadiness struct {
	rows []custody.ReadinessRow
	err  error
}

func (f fakeSecretReadiness) Readiness(context.Context, custody.ReadinessRequest) ([]custody.ReadinessRow, error) {
	return f.rows, f.err
}

type fakeMediaRecords struct {
	held map[string]installation.MediaRecord
	err  error
}

func (f fakeMediaRecords) MediaRecords(context.Context) (map[string]installation.MediaRecord, error) {
	return f.held, f.err
}

func readinessCatalog() api.Catalog {
	return api.NewCatalog([]api.Object{
		api.NewObject(api.MachineImage, "rhel", api.Value{}, api.MapValue(api.FieldValue{Name: "bootMedia", Value: api.StringValue("local-media:rhel.iso")})),
		api.NewObject(api.MachineImage, "held", api.Value{}, api.MapValue(api.FieldValue{Name: "bootMedia", Value: api.StringValue("local-media:held.iso")})),
	})
}

func TestStatusReadinessListsMissingSecretsThenMediaTheStoreLacks(t *testing.T) {
	secrets := fakeSecretReadiness{rows: []custody.ReadinessRow{
		{Name: "key", Status: "stale", Remedy: "bootwright secret generate --context lab --name key"},
		{Name: "token", Status: "missing", Remedy: "bootwright secret set --context lab --name token --value-file <path>"},
	}}
	media := fakeMediaRecords{held: map[string]installation.MediaRecord{"held.iso": {}}}
	gaps, err := statusReadiness(secrets, media)(context.Background(), "lab", readinessCatalog())
	want := []lifecycle.ReadinessGap{
		{Kind: "secret", Name: "key", State: "stale", Remedy: "bootwright secret generate --context lab --name key"},
		{Kind: "secret", Name: "token", State: "missing", Remedy: "bootwright secret set --context lab --name token --value-file <path>"},
		{Kind: "media", Name: "rhel.iso", State: "missing", Remedy: "bootwright media add --name rhel.iso --from-file <path>"},
	}
	if err != nil || !slices.Equal(gaps, want) {
		t.Fatalf("gaps = %v (%v), want %v", gaps, err, want)
	}
}

func TestStatusReadinessNamesNothingWhenNothingIsMissing(t *testing.T) {
	media := fakeMediaRecords{held: map[string]installation.MediaRecord{"rhel.iso": {}, "held.iso": {}}}
	gaps, err := statusReadiness(fakeSecretReadiness{}, media)(context.Background(), "lab", readinessCatalog())
	if err != nil || len(gaps) != 0 {
		t.Fatalf("gaps = %v (%v), want none", gaps, err)
	}
}

func TestStatusReadinessKeepsOneStoresGapsWhenTheOtherFails(t *testing.T) {
	broken := errors.New("store unreadable")
	secrets := fakeSecretReadiness{rows: []custody.ReadinessRow{{Name: "token", Status: "missing", Remedy: "bootwright secret set --context lab --name token --value-file <path>"}}}
	gaps, err := statusReadiness(secrets, fakeMediaRecords{err: broken})(context.Background(), "lab", readinessCatalog())
	if err != nil || len(gaps) != 1 || gaps[0].Kind != "secret" {
		t.Fatalf("a failed media read hid the Secret gap: %v (%v)", gaps, err)
	}
	gaps, err = statusReadiness(fakeSecretReadiness{err: broken}, fakeMediaRecords{held: map[string]installation.MediaRecord{}})(context.Background(), "lab", readinessCatalog())
	if err != nil || len(gaps) != 2 || gaps[0].Kind != "media" {
		t.Fatalf("a failed Secret read hid the media gaps: %v (%v)", gaps, err)
	}
	if gaps, err = statusReadiness(fakeSecretReadiness{err: broken}, fakeMediaRecords{err: broken})(context.Background(), "lab", readinessCatalog()); err == nil || gaps != nil {
		t.Fatalf("two failed reads named %v (%v), want the failure", gaps, err)
	}
}
