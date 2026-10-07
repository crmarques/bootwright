package ansiblelocal

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// recordingExecution runs the Ansible under whatever requirement it was asked
// to verify, and keeps that requirement.
type recordingExecution struct {
	verified *prerequisites.ExecutionRequirement
}

func (execution recordingExecution) WithPython(_ context.Context, _ prerequisites.BundleArea, requirement prerequisites.ExecutionRequirement, run func(prerequisites.PythonLaunch, func() error) error) error {
	*execution.verified = requirement
	return run(prerequisites.PythonLaunch{Loader: "/bundle/python/lib/ld.so", Directory: "/bundle"}, func() error { return nil })
}

// Setup's own Ansible verifies the execution foundation setup's inspection
// qualified on an errata host, under the definition's interpreter, and the
// definition's own requirement otherwise.
func TestSetupsAnsibleVerifiesTheQualifiedFoundation(t *testing.T) {
	compiled := prerequisites.ExecutionRequirement{PythonExecutable: "python/bin/python3.14", Loader: "/usr/lib64/ld-linux-x86-64.so.2", LockPath: "/var/lib/rpm/.rpm.lock",
		Files:   []prerequisites.InstalledFile{{Path: "/usr/lib64/ld-linux-x86-64.so.2", SHA256: strings.Repeat("1", 64)}, {Path: "/usr/lib64/libc.so.6", SHA256: strings.Repeat("2", 64)}},
		Links:   []prerequisites.InstalledLink{},
		Preload: []string{"/usr/lib64/libc.so.6"}}
	foundation := &prerequisites.QualifiedFoundation{
		Execution: prerequisites.ExecutionRequirement{Loader: compiled.Loader, LockPath: compiled.LockPath,
			Files: []prerequisites.InstalledFile{compiled.Files[0], {Path: "/usr/lib64/libc.so.6", SHA256: strings.Repeat("3", 64)}}, Links: []prerequisites.InstalledLink{}, Preload: []string{"/usr/lib64/libc.so.6"}},
		Packages: []prerequisites.FoundationBuild{{Name: "glibc", Build: "2.34-276.el9_8", Files: []string{"/usr/lib64/ld-linux-x86-64.so.2", "/usr/lib64/libc.so.6"}}},
	}
	qualified := foundation.Execution
	qualified.PythonExecutable = compiled.PythonExecutable
	area := authorityArea{location: prerequisites.BundleLocation{Path: "/bundle", Writable: true}}
	definition := prerequisites.Definition{CatalogDigest: strings.Repeat("a", 64), Execution: compiled}
	publish := func(context.Context, prerequisites.NativePreparation) error { return nil }
	for name, test := range map[string]struct {
		ctx  context.Context
		want prerequisites.ExecutionRequirement
	}{
		"an errata host":      {ctx: prerequisites.WithLaunchFoundation(context.Background(), foundation), want: qualified},
		"the compiled builds": {ctx: prerequisites.WithLaunchFoundation(context.Background(), nil), want: compiled},
		"no setup inspection": {ctx: context.Background(), want: compiled},
	} {
		t.Run(name, func(t *testing.T) {
			var verified prerequisites.ExecutionRequirement
			installer := New(recordingExecution{verified: &verified})
			installer.runner = func(context.Context, prerequisites.PythonLaunch, capabilityRequest, func() error, func(context.Context, prerequisites.NativePreparation) error, func(prerequisites.ProgressEvent), prerequisites.RunOutput) (prerequisites.ActionResult, error) {
				return actionResult("changed", true), nil
			}
			if _, err := installer.Prepare(test.ctx, area, prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, publish, nil, nil); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(verified, test.want) {
				t.Fatalf("setup's Ansible verified %+v, want %+v", verified, test.want)
			}
		})
	}
}
