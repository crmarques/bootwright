package prerequisites

import (
	"context"
	"errors"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type fipsProbe struct {
	enabled bool
	err     error
	calls   int
}

func (p *fipsProbe) FIPSMode(context.Context) (bool, error) { p.calls++; return p.enabled, p.err }
func fipsCatalog() api.Catalog {
	return api.NewCatalog([]api.Object{api.NewObject(api.ContainerCluster, "fixture", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "security", "fips", "enabled"))})
}
func TestSelectedFIPSClusterRequiresPositiveKernelObservation(t *testing.T) {
	for _, p := range []*fipsProbe{nil, {}, {enabled: true, err: errors.New("unreadable")}, {enabled: true}} {
		var inspector FIPSInspector
		if p != nil {
			inspector = p
		}
		err := CheckClusterFIPS(t.Context(), inspector, fipsCatalog())
		if p != nil && p.enabled && p.err == nil {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		got := diagnostics.Of(err)
		if len(got) != 1 || got[0].Code != "controller.unsupported" {
			t.Fatal(err)
		}
	}
	p := &fipsProbe{}
	if err := CheckClusterFIPS(t.Context(), p, api.NewCatalog(nil)); err != nil || p.calls != 0 {
		t.Fatal("ordinary graph read FIPS mode", err, p.calls)
	}
}
func TestPreflightFIPSFailurePrecedesFurtherHostEffects(t *testing.T) {
	f := newSetupFixture(t)
	current := inspection{catalog: fipsCatalog()}
	err := f.service.settleHost(t.Context(), &current, func(Check) {}, func(string, string) {})
	if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != "controller.unsupported" {
		t.Fatal(err)
	}
}
