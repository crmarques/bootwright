//go:build linux && amd64 && controllerqualification

package bundlelocal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/hostlinux"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// This current-OS check runs only the unprivileged disposable resolver. It does
// not install host dependencies, publish a bundle, or claim root acceptance.
func TestQualifiedLatestBootstrapResolution(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_QUALIFY_DYNAMIC_BOOTSTRAP") != "1" {
		t.Skip("explicit current-OS publisher resolution gate")
	}
	if os.Getuid() == 0 {
		t.Fatal("this qualification must run as an unprivileged host user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	platform, err := hostlinux.New().Platform(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewBootstrapResolver()
	if fixtures := os.Getenv("BOOTWRIGHT_BOOTSTRAP_SOURCE_FIXTURES"); fixtures != "" {
		if err := os.MkdirAll(fixtures, 0700); err != nil {
			t.Fatal(err)
		}
		resolver.fetch = func(ctx context.Context, source prerequisites.DependencySource, egress prerequisites.SetupEgress) ([]byte, error) {
			filename := filepath.Join(fixtures, source.ID)
			if data, err := os.ReadFile(filename); err == nil && approvedBytes(source, data) {
				return data, nil
			}
			data, err := fetchSource(ctx, source, egress)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(filename, data, 0600); err != nil {
				return nil, err
			}
			return data, nil
		}
	}
	resolved, err := resolver.Resolve(ctx, platform, controller.DefaultDependencyVersions(), prerequisites.SetupEgress{})
	if err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
	if err := prerequisites.ValidateBootstrap(resolved); err != nil {
		t.Fatal(err)
	}
	t.Logf("resolved Python %s, Ansible %s, %d exact publisher sources, %d projected files", resolved.PythonVersion, resolved.AnsibleVersion, len(resolved.Sources), resolved.FileCount)
	if output := os.Getenv("BOOTWRIGHT_RESOLVED_BOOTSTRAP_OUTPUT"); output != "" {
		data, err := json.MarshalIndent(resolved, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQualifiedResolvedBootstrapPreparation(t *testing.T) {
	bootstrapPath, nativePath, fixtures := os.Getenv("BOOTWRIGHT_RESOLVED_BOOTSTRAP_OUTPUT"), os.Getenv("BOOTWRIGHT_RESOLVED_NATIVE_INPUT"), os.Getenv("BOOTWRIGHT_BOOTSTRAP_SOURCE_FIXTURES")
	if bootstrapPath == "" || nativePath == "" || fixtures == "" {
		t.Skip("explicit independently resolved manifests and source fixtures are required")
	}
	if os.Getuid() == 0 {
		t.Fatal("bootstrap artifact/import qualification must run as an unprivileged host user")
	}
	var bootstrap prerequisites.BootstrapDefinition
	var native prerequisites.NativeResolvedPlan
	for filename, target := range map[string]any{bootstrapPath: &bootstrap, nativePath: &native} {
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatal(err)
		}
	}
	definition, err := prerequisites.NewResolvedDefinition(bootstrap, native)
	if err != nil {
		t.Fatal(err)
	}
	area := newQualificationArea(t)
	manager := New(ExecutionGuard{})
	fetches := 0
	manager.fetch = func(ctx context.Context, source prerequisites.DependencySource, _ prerequisites.SetupEgress) ([]byte, error) {
		fetches++
		data, err := os.ReadFile(filepath.Join(fixtures, source.ID))
		if err != nil {
			return nil, err
		}
		if !approvedBytes(source, data) {
			return nil, bundleFailure("qualification source changed")
		}
		return data, ctx.Err()
	}
	// Execute the actual resolved import probe as the invoking host user. This
	// seam grants no root ownership bypass to production Manager/ExecutionGuard.
	manager.probe = func(ctx context.Context, area prerequisites.BundleArea, definition prerequisites.Definition) error {
		location, err := area.Location(ctx)
		if err != nil {
			return err
		}
		return runImportProbe(ctx, area, prerequisites.PythonLaunch{Loader: definition.Execution.Loader, Arguments: []string{"--inhibit-cache", "--glibc-hwcaps-mask", "", "--library-path", filepath.Join(location.Path, "python/lib"), "--preload", strings.Join(definition.Execution.Preload, ":"), filepath.Join(location.Path, definition.Bootstrap.PythonExecutable)}, Directory: location.Path, Environment: []string{"LC_ALL=C.UTF-8", "LANG=C.UTF-8", "HOME=" + location.Path, "OPENSSL_CONF=/dev/null"}}, definition)
	}
	if err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil); err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
	before, err := area.Entries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	writes, acquisitions := area.writes, fetches
	for _, execute := range []bool{false, true} {
		inspection, err := manager.Inspect(t.Context(), area, definition, execute)
		if err != nil || !inspection.Ready || !inspection.Recoverable {
			t.Fatalf("resolved inspection failed: %+v %v", inspection, err)
		}
	}
	if err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil); err != nil {
		t.Fatal(err)
	}
	after, err := area.Entries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if writes != area.writes || acquisitions != fetches || !slices.Equal(before, after) {
		t.Fatal("resolved replay changed files or reacquired dependencies")
	}
	t.Logf("combined real current-OS manifests: %d projected bundle entries; actual Python %s / Ansible %s imports; read-only replay; zero native package actions applied", len(after), bootstrap.PythonVersion, bootstrap.AnsibleVersion)
}
