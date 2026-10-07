package lifecycle

import (
	"context"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// receiptView is a View whose controller holds one receipt and one bundle area.
type receiptView struct {
	View
	controller prerequisites.StorageView
}

func (v receiptView) Controller() prerequisites.StorageView { return v.controller }

func (v receiptView) Identity() ContextIdentity { return ContextIdentity{Name: "lab"} }

type locatedArea struct{ prerequisites.BundleArea }

func (locatedArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return prerequisites.BundleLocation{Path: "/var/lib/bootwright/controller/bundles/one", Device: 1, Inode: 2}, nil
}

// Every launch of an operation verifies the execution foundation its
// controller's receipt records: the one setup qualified from the RPM database
// when the receipt carries it, under the definition's interpreter, and the
// definition's own otherwise.
func TestALaunchUsesTheReceiptsQualifiedFoundation(t *testing.T) {
	compiled := prerequisites.ExecutionRequirement{PythonExecutable: "python/bin/python3.14", Loader: "/usr/lib64/ld-linux-x86-64.so.2", LockPath: "/var/lib/rpm/.rpm.lock",
		Files:   []prerequisites.InstalledFile{{Path: "/usr/lib64/ld-linux-x86-64.so.2", SHA256: "1111111111111111111111111111111111111111111111111111111111111111"}, {Path: "/usr/lib64/libgcc_s-11-20240719.so.1", SHA256: "2222222222222222222222222222222222222222222222222222222222222222"}},
		Links:   []prerequisites.InstalledLink{{Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-11-20240719.so.1"}},
		Preload: []string{"/usr/lib64/libgcc_s-11-20240719.so.1"}}
	foundation := &prerequisites.QualifiedFoundation{
		Execution: prerequisites.ExecutionRequirement{Loader: compiled.Loader, LockPath: compiled.LockPath,
			Files:   []prerequisites.InstalledFile{compiled.Files[0], {Path: "/usr/lib64/libgcc_s-11-20250101.so.1", SHA256: "3333333333333333333333333333333333333333333333333333333333333333"}},
			Links:   []prerequisites.InstalledLink{{Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-11-20250101.so.1"}},
			Preload: []string{"/usr/lib64/libgcc_s-11-20250101.so.1"}},
		Packages: []prerequisites.FoundationBuild{{Name: "glibc", Build: "2.34-276.el9_8", Files: []string{"/usr/lib64/ld-linux-x86-64.so.2"}}, {Name: "libgcc", Build: "11.5.0-15.el9", Files: []string{"/usr/lib64/libgcc_s-11-20250101.so.1"}}},
	}
	open := func(context.Context, string) (prerequisites.BundleArea, error) { return locatedArea{}, nil }
	for name, test := range map[string]struct {
		foundation *prerequisites.QualifiedFoundation
		want       prerequisites.ExecutionRequirement
	}{
		"qualified": {foundation: foundation, want: func() prerequisites.ExecutionRequirement {
			want := foundation.Execution
			want.PythonExecutable = compiled.PythonExecutable
			return want
		}()},
		"compiled": {want: compiled},
	} {
		t.Run(name, func(t *testing.T) {
			definition := prerequisites.Definition{Execution: compiled}
			receipt := prerequisites.SetupReceipt{CatalogDigest: "digest", Definition: &definition, Foundation: test.foundation, Status: "complete"}
			view := receiptView{controller: prerequisites.StorageView{State: prerequisites.HostState{Receipt: receipt}, OpenBundle: open}}
			approved, err := approvedBundle(context.Background(), view)
			if err != nil || !reflect.DeepEqual(approved.requirement, test.want) {
				t.Fatalf("the launch verifies %+v (%v), want %+v", approved.requirement, err, test.want)
			}
		})
	}
}
