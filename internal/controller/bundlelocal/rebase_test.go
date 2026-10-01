package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const rebasePython = "3.13.15"

// retainedBootstrap is one complete publisher resolution whose sources are
// small synthetic archives, so a reprojection can be computed exactly rather
// than asserted against a hash nothing in the test produced. Its automation
// digest is deliberately not this executable's.
func retainedBootstrap(t *testing.T) (prerequisites.BootstrapDefinition, *memoryArea) {
	t.Helper()
	record, _, err := compiledCatalog()
	if err != nil {
		t.Fatal(err)
	}
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	profile, ok := selectNative(record, platform)
	if !ok {
		t.Fatal("fixture platform missing")
	}
	payloads := [][]byte{
		pythonArchive(t, archiveMember{name: "python/bin/python" + minorOf(rebasePython), data: "interpreter"}),
		wheelArchive(t, "ansible_core/__init__.py"),
		wheelArchive(t, "urllib3/__init__.py"),
	}
	sources := []prerequisites.DependencySource{
		published("python-"+rebasePython, "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-"+rebasePython+"+20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz", payloads[0]),
		published("wheel-ansible-core", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl", payloads[1]),
		published("wheel-urllib3", "https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl", payloads[2]),
	}
	value := prerequisites.BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: platform,
		PythonIntent: "latest", AnsibleIntent: "latest", PythonVersion: rebasePython, AnsibleVersion: "2.21.4",
		PythonExecutable: "python/bin/python" + minorOf(rebasePython),
		SitePackages:     "python/lib/python" + minorOf(rebasePython) + "/site-packages/",
		Sources:          sources,
		Wheels: []prerequisites.BootstrapWheel{
			{Name: "ansible-core", Version: "2.21.4", SourceID: sources[1].ID},
			{Name: "urllib3", Version: "2.7.0", SourceID: sources[2].ID},
		},
		Metadata: []prerequisites.DependencySource{
			published("python-metadata", pythonMetadataURL, []byte("python metadata")),
			published("ansible-metadata", "https://pypi.org/pypi/ansible-core/json", []byte("ansible metadata")),
		},
		Execution: cloneExecution(profile.Execution), ExecutionPackages: executionPackageOwners(),
		// A superseded revision and a projection identity that belongs to it.
		AutomationDigest: strings.Repeat("f", 64), ProjectionSHA256: strings.Repeat("e", 64),
		FileCount: 1, ExpandedBytes: 1,
	}
	value.Execution.PythonExecutable = value.PythonExecutable
	value, err = prerequisites.CanonicalBootstrap(value)
	if err != nil {
		t.Fatal(err)
	}
	area := newMemoryArea()
	for index, source := range value.Sources {
		area.files[sourcePath(source)] = projectedFile{data: payloads[index]}
	}
	area.writes = 0
	return value, area
}

func minorOf(version string) string { return version[:strings.LastIndex(version, ".")] }

func published(id, url string, data []byte) prerequisites.DependencySource {
	digest := sha256.Sum256(data)
	return prerequisites.DependencySource{ID: id, URL: url, SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data))}
}

// currentProjection is the identity the retained sources have under the
// automation this executable embeds, computed the same way preparation and
// inspection compute it.
func currentProjection(t *testing.T, value prerequisites.BootstrapDefinition, area *memoryArea) (string, int, int64) {
	t.Helper()
	projected := newProjection()
	projected.site = value.SitePackages
	if err := projected.automation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for index, source := range value.Sources {
		if err := projectSource(t.Context(), projected, index, area.files[sourcePath(source)].data); err != nil {
			t.Fatal(err)
		}
	}
	return projected.identity(), len(projected.files), projected.bytes
}

// An automation revision moves the bundle a host must publish, never the
// dependencies it resolved. Rebasing therefore keeps every release, source,
// byte count and publisher origin and reads them from the bundle this host
// already holds, so nothing is acquired and nothing is written.
func TestRebaseKeepsEveryRetainedIdentityAndOnlyMovesTheAutomation(t *testing.T) {
	retained, area := retainedBootstrap(t)
	rebased, err := New(nil).Rebase(t.Context(), area, retained)
	if err != nil {
		t.Fatal(err)
	}
	if rebased.AutomationDigest != ansible.Digest() {
		t.Fatal("rebasing did not adopt the embedded automation")
	}
	if !slices.Equal(rebased.Sources, retained.Sources) || !slices.Equal(rebased.Metadata, retained.Metadata) ||
		!slices.Equal(rebased.Wheels, retained.Wheels) || rebased.PythonVersion != retained.PythonVersion ||
		rebased.AnsibleVersion != retained.AnsibleVersion || rebased.PythonIntent != retained.PythonIntent ||
		!equalExecution(rebased.Execution, retained.Execution) {
		t.Fatalf("rebasing moved more than the automation: %#v", rebased)
	}
	identity, files, bytes := currentProjection(t, retained, area)
	if rebased.ProjectionSHA256 != identity || rebased.FileCount != files || rebased.ExpandedBytes != bytes {
		t.Fatalf("reprojection = %s/%d/%d; want %s/%d/%d", rebased.ProjectionSHA256, rebased.FileCount, rebased.ExpandedBytes, identity, files, bytes)
	}
	if rebased.Digest == retained.Digest || prerequisites.ValidateBootstrap(rebased) != nil {
		t.Fatal("the reprojected resolution has no valid identity of its own")
	}
	if area.writes != 0 {
		t.Fatal("rebasing wrote to the retained bundle")
	}
}

// A resolution retained before documentation left the digest recorded it over
// every embedded file under the first domain version. No such digest equals
// this build's, so its definition is superseded before any identity written
// under the old rule is compared, and rebasing carries it forward under the
// narrowed projection: two files and their bytes fewer than a projection that
// still counted documentation.
func TestRebaseCarriesAV1DigestForwardUnderTheV2Identity(t *testing.T) {
	retained, area := retainedBootstrap(t)
	retained.AutomationDigest = automationDigestV1(ansible.Assets())
	retained, err := prerequisites.CanonicalBootstrap(retained)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(retained, *resolvedDefinitionFixture(t).Native)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateDefinition(definition); !errors.Is(err, prerequisites.ErrAutomationSuperseded) {
		t.Fatalf("a definition retained under the first digest domain was not superseded: %v", err)
	}
	rebased, err := New(nil).Rebase(t.Context(), area, retained)
	if err != nil {
		t.Fatal(err)
	}
	counted := newProjection()
	counted.site = retained.SitePackages
	assets := ansible.Assets()
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		if err := counted.add(path.Join("automation", name), assets[name], false); err != nil {
			t.Fatal(err)
		}
	}
	for index, source := range retained.Sources {
		if err := projectSource(t.Context(), counted, index, area.files[sourcePath(source)].data); err != nil {
			t.Fatal(err)
		}
	}
	documented := int64(0)
	for _, data := range ansible.Documentation() {
		documented += int64(len(data))
	}
	if rebased.AutomationDigest != ansible.Digest() || rebased.FileCount != len(counted.files)-2 || rebased.ExpandedBytes != counted.bytes-documented {
		t.Fatalf("rebased %s over %d files and %d bytes; want %s over %d and %d",
			rebased.AutomationDigest, rebased.FileCount, rebased.ExpandedBytes, ansible.Digest(), len(counted.files)-2, counted.bytes-documented)
	}
}

// automationDigestV1 is the automation digest as builds before this one
// recorded it: every embedded file under the first domain version.
func automationDigestV1(files map[string][]byte) string {
	digest := sha256.New()
	digest.Write([]byte("bootwright.controller.automation-v1\x00"))
	for _, name := range slices.Sorted(maps.Keys(files)) {
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		content := sha256.Sum256(files[name])
		digest.Write(content[:])
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// The retained bundle is evidence, not authority: bytes that are no longer the
// approved ones cannot be carried onto a new automation revision. A source the
// bundle lost or changed is reported as one it cannot serve, which setup
// settles by resolving afresh; a canceled read stays a cancellation.
func TestRebaseRefusesRetainedSourcesThatAreNotTheirApprovedBytes(t *testing.T) {
	retained, _ := retainedBootstrap(t)
	for _, source := range retained.Sources {
		t.Run(source.ID, func(t *testing.T) {
			_, corrupted := retainedBootstrap(t)
			corrupted.files[sourcePath(source)] = projectedFile{data: []byte("replacement payload")}
			if _, err := New(nil).Rebase(t.Context(), corrupted, retained); !errors.Is(err, prerequisites.ErrRetainedSourceUnavailable) {
				t.Fatalf("a retained source that changed was not reported as unservable: %v", err)
			}
			_, missing := retainedBootstrap(t)
			delete(missing.files, sourcePath(source))
			if _, err := New(nil).Rebase(t.Context(), missing, retained); !errors.Is(err, prerequisites.ErrRetainedSourceUnavailable) {
				t.Fatalf("a retained bundle that lost a source was not reported as unservable: %v", err)
			}
		})
	}
	_, held := retainedBootstrap(t)
	ctx, cancel := context.WithCancel(t.Context())
	_, err := New(nil).Rebase(ctx, cancelingArea{memoryArea: held, cancel: cancel}, retained)
	if !errors.Is(err, context.Canceled) || errors.Is(err, prerequisites.ErrRetainedSourceUnavailable) {
		t.Fatalf("a canceled read was reported as a source the bundle cannot serve: %v", err)
	}
}

// cancelingArea is a retained bundle whose reader is canceled mid-read.
type cancelingArea struct {
	*memoryArea
	cancel context.CancelFunc
}

func (a cancelingArea) Read(ctx context.Context, _ string, _ int) ([]byte, error) {
	a.cancel()
	return nil, ctx.Err()
}

// A provided execution foundation is host evidence. No reprojection can repair
// it, so it refuses here exactly as it refuses when a bundle is validated.
func TestRebaseRefusesADifferentProvidedFoundation(t *testing.T) {
	retained, area := retainedBootstrap(t)
	retained.Execution.Loader = "/lib64/ld-linux-x86-64.so.9"
	retained, err := prerequisites.CanonicalBootstrap(retained)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(nil).Rebase(t.Context(), area, retained)
	if !errors.Is(err, prerequisites.ErrBootstrapIncompatible) || errors.Is(err, prerequisites.ErrAutomationSuperseded) {
		t.Fatalf("a changed foundation was reported as a settleable automation revision: %v", err)
	}
}

// Carrying a resolution forward is only worth doing if it acquires nothing, so
// preparation reads every approved source from the bundle the host already
// holds and names that recovery in its progress.
func TestPreparationRecoversRetainedSourcesInsteadOfAcquiringThem(t *testing.T) {
	retained, held := retainedBootstrap(t)
	manager := New(nil)
	probes := 0
	manager.probe = func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error {
		probes++
		return nil
	}
	manager.fetch = func(_ context.Context, source prerequisites.DependencySource, _ prerequisites.SetupEgress) ([]byte, error) {
		t.Fatalf("acquired %s although this host retains it", source.ID)
		return nil, nil
	}
	rebased, err := manager.Rebase(t.Context(), held, retained)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(rebased, *resolvedDefinitionFixture(t).Native)
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	area := newMemoryArea()
	published, err := manager.Prepare(t.Context(), area, held, definition, prerequisites.SetupEgress{}, func(event prerequisites.ProgressEvent) {
		details = append(details, event.Detail)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Preparation returns what it verified, so the closure is read back and the
	// interpreter qualified once rather than again by the caller.
	if !published.Ready || !published.Recoverable || probes != 1 {
		t.Fatalf("preparation returned %+v after %d interpreter probes", published, probes)
	}
	recovered := 0
	for _, detail := range details {
		if strings.HasPrefix(detail, "recovering ") {
			recovered++
		}
	}
	if recovered != len(rebased.Sources) {
		t.Fatalf("recovered %d of %d retained sources: %q", recovered, len(rebased.Sources), details)
	}
	inspection, _, err := inspectFiles(t.Context(), area, mustValidate(t, definition))
	if err != nil || !inspection.Ready {
		t.Fatalf("the published bundle is not complete: %+v %v", inspection, err)
	}
}

func mustValidate(t *testing.T, definition prerequisites.Definition) catalogRecord {
	t.Helper()
	record, err := validateDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
