//go:build linux && amd64

package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/nativelocal"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// verifyingGuard keeps the requirement a launch asked it to verify and runs
// nothing.
type verifyingGuard struct {
	verified []prerequisites.ExecutionRequirement
}

var errNotLaunched = errors.New("not launched")

func (g *verifyingGuard) WithPython(_ context.Context, _ prerequisites.BundleArea, requirement prerequisites.ExecutionRequirement, _ func(prerequisites.PythonLaunch, func() error) error) error {
	g.verified = append(g.verified, requirement)
	return errNotLaunched
}

// requalified is the foundation setup would record for requirement on a host
// where the file at index holds digest instead.
func requalified(requirement prerequisites.ExecutionRequirement, index int, digest string) *prerequisites.QualifiedFoundation {
	execution := cloneExecution(requirement)
	execution.PythonExecutable = ""
	execution.Files[index].SHA256 = digest
	foundation := &prerequisites.QualifiedFoundation{Execution: execution}
	native, _, _ := compiledNativeFor(requirement)
	for _, pkg := range native.Packages {
		foundation.Packages = append(foundation.Packages, prerequisites.FoundationBuild{Name: pkg.Name, Build: pkg.Build, Files: pkg.Files})
	}
	return foundation
}

// The bundle probe verifies the foundation setup's inspection qualified, and
// the definition's own requirement when setup qualified none.
func TestTheBundleProbeVerifiesTheQualifiedFoundation(t *testing.T) {
	compiled := compiledExecution(t, rhelPlatform)
	compiled.PythonExecutable = "python/bin/python3.14"
	foundation := requalified(compiled, 1, strings.Repeat("e", 64))
	qualified := cloneExecution(foundation.Execution)
	qualified.PythonExecutable = compiled.PythonExecutable
	definition := prerequisites.Definition{Execution: compiled}
	guard := &verifyingGuard{}
	if err := probeBundle(prerequisites.WithLaunchFoundation(context.Background(), foundation), guard, nil, definition); !errors.Is(err, errNotLaunched) {
		t.Fatal(err)
	}
	if err := probeBundle(context.Background(), guard, nil, definition); !errors.Is(err, errNotLaunched) {
		t.Fatal(err)
	}
	if len(guard.verified) != 2 || !reflect.DeepEqual(guard.verified[0], qualified) || !reflect.DeepEqual(guard.verified[1], compiled) {
		t.Fatalf("the probe verified %+v", guard.verified)
	}
}

// The resolver's staging copies the foundation setup qualified: on a host
// whose libc the definition no longer pins, the qualified digest stages it
// and the definition's alone refuses it.
func TestTheResolverStagesTheQualifiedFoundation(t *testing.T) {
	host := compiledExecution(t, fedoraPlatform)
	host.PythonExecutable = "python/bin/python3.13"
	for _, file := range host.Files {
		data, err := os.ReadFile(file.Path)
		digest := sha256.Sum256(data)
		if err != nil || hex.EncodeToString(digest[:]) != file.SHA256 {
			t.Skip("this host does not hold the compiled Fedora 43 foundation")
		}
	}
	definition := cloneExecution(host)
	index := 1
	definition.Files[index].SHA256 = strings.Repeat("0", 64)
	foundation := requalified(definition, index, host.Files[index].SHA256)
	projected := newProjection()
	if err := projected.add(host.PythonExecutable, []byte("interpreter"), true); err != nil {
		t.Fatal(err)
	}
	value := prerequisites.BootstrapDefinition{PythonExecutable: host.PythonExecutable, Execution: definition}
	staging := nativelocal.NewStaging(filepath.Join(t.TempDir(), "staging"))
	if _, _, err := stageBootstrap(t.Context(), staging, projected, value, []byte("trust")); err == nil {
		t.Fatal("the definition's own requirement staged a libc it does not pin")
	}
	root, stage, err := stageBootstrap(prerequisites.WithLaunchFoundation(t.Context(), foundation), staging, projected, value, []byte("trust"))
	if err != nil {
		t.Fatalf("the qualified foundation did not stage: %#v", diagnostics.Of(err))
	}
	defer stage.Release()
	if data, err := os.ReadFile(filepath.Join(root, strings.TrimPrefix(host.Files[index].Path, "/"))); err != nil || len(data) == 0 {
		t.Fatalf("the qualified libc was not staged: %v", err)
	}
}

// A fresh resolution on a host whose glibc or libgcc setup requalified proves
// its projection against that qualified foundation, not the compiled digests
// the host no longer holds, while the definition still records the compiled
// requirement.
func TestAResolutionQualifiesItsProjectionAgainstTheQualifiedFoundation(t *testing.T) {
	compiled := compiledExecution(t, fedoraPlatform)
	index := 1
	errata := strings.Repeat("e", 64)
	foundation := requalified(compiled, index, errata)
	fetched, qualified := 0, 0
	resolver := syntheticResolver(t, "1."+strconv.Itoa(indexAPIMinor), &fetched, &qualified)
	var proved prerequisites.ExecutionRequirement
	resolver.qualify = func(_ *projection, requirement prerequisites.ExecutionRequirement) error {
		qualified++
		proved = requirement
		return nil
	}
	ctx := prerequisites.WithLaunchFoundation(t.Context(), foundation)
	resolved, _, err := resolver.Resolve(ctx, fedoraPlatform, controller.DefaultDependencyVersions(), prerequisites.SetupEgress{})
	if err != nil || qualified != 1 {
		t.Fatalf("the resolution did not complete: %d qualified %+v", qualified, diagnostics.Of(err))
	}
	if proved.Files[index].SHA256 != errata || proved.PythonExecutable != resolved.PythonExecutable {
		t.Fatalf("the projection was qualified against %+v, not the qualified foundation", proved)
	}
	if resolved.Execution.Files[index].SHA256 != compiled.Files[index].SHA256 {
		t.Fatalf("the definition recorded %+v, not the compiled requirement", resolved.Execution.Files[index])
	}
}
