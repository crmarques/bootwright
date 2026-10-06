//go:build linux && amd64

// Package hostlinux inspects fixed local Linux evidence without subprocesses,
// network access, package database access, or filesystem mutation.
package hostlinux

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"golang.org/x/sys/unix"
)

const maxMetadataBytes = 1 << 20

// Inspector implements read-only inspection of the executing installed host.
// New fixes every production path; there is no operator-selected filesystem.
type Inspector struct{ view filesystemView }

// New binds inspection to the real local filesystem and compiled architecture.
func New() Inspector {
	return Inspector{view: filesystemView{root: "/", owner: 0, architecture: runtime.GOARCH}}
}

var _ prerequisites.HostInspector = Inspector{}

// Platform reads only the trusted OS declaration. It does not qualify the
// release, inspect desired state, or create the Bootwright store.
func (i Inspector) Platform(ctx context.Context) (prerequisites.Platform, error) {
	fs, err := i.view.open(ctx)
	if err != nil {
		return prerequisites.Platform{}, inspectionFailure(ctx, "controller.unsupported", "host platform metadata cannot be inspected safely")
	}
	defer fs.close()
	data, err := fs.read(ctx, "/etc/os-release", 64<<10)
	if err != nil {
		return prerequisites.Platform{}, inspectionFailure(ctx, "controller.unsupported", "host OS declaration is missing or unsafe")
	}
	id, release, err := parseOSRelease(data)
	if err != nil {
		return prerequisites.Platform{}, inspectionFailure(ctx, "controller.unsupported", "host OS declaration is malformed")
	}
	return prerequisites.Platform{OS: id, Release: release, Architecture: i.view.architecture}, nil
}

// Identity acquires the canonical installed-host tuple and independently checks
// observable execution boundaries. Identical full clones remain indistinguishable.
func (i Inspector) Identity(ctx context.Context) (controller.InstalledHostIdentity, error) {
	fs, err := i.view.open(ctx)
	if err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	defer fs.close()
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv", "/run/systemd/container"} {
		file, _, err := fs.openPath(ctx, marker, false)
		if file != nil {
			file.Close()
		}
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return controller.InstalledHostIdentity{}, identityFailure(ctx)
		}
	}
	proc, err := fs.kernelRoot(ctx, "/proc", unix.PROC_SUPER_MAGIC)
	if err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	defer proc.Close()
	if err := fs.sameMountNamespace(ctx, proc); err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	status, err := fs.readKernel(ctx, proc, "self/status", maxMetadataBytes)
	if err != nil || nestedPIDNamespace(status) {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	cgroups, err := fs.readKernel(ctx, proc, "1/cgroup", maxMetadataBytes)
	if err != nil || containerCgroup(cgroups) {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	mountinfo, err := fs.readKernel(ctx, proc, "self/mountinfo", maxMetadataBytes)
	if err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	mount, err := rootMount(mountinfo)
	if err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	filesystemUUID, err := fs.filesystemUUID(ctx, mount)
	if err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	machineID, err := fs.read(ctx, "/etc/machine-id", 128)
	if err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	sys, err := fs.kernelRoot(ctx, "/sys", unix.SYSFS_MAGIC)
	if err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	sys.Close()
	product, err := fs.read(ctx, "/sys/class/dmi/id/product_uuid", 128)
	if err != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	identity, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, trimTerminator(machineID), strings.ToLower(trimTerminator(product)), filesystemUUID)
	if err != nil || ctx.Err() != nil {
		return controller.InstalledHostIdentity{}, identityFailure(ctx)
	}
	return identity, nil
}

func parseOSRelease(data []byte) (string, string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || key != "ID" && key != "VERSION_ID" {
			continue
		}
		if _, duplicate := values[key]; duplicate {
			return "", "", errEvidence
		}
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		if value == "" {
			return "", "", errEvidence
		}
		for _, c := range value {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
				return "", "", errEvidence
			}
		}
		values[key] = value
	}
	if values["ID"] == "" || values["VERSION_ID"] == "" {
		return "", "", errEvidence
	}
	return values["ID"], values["VERSION_ID"], nil
}

func trimTerminator(data []byte) string { return strings.TrimSuffix(string(data), "\n") }

var errEvidence = errors.New("host evidence cannot be verified")

func identityFailure(ctx context.Context) error {
	return inspectionFailure(ctx, "controller.identity", "installed-host identity or execution boundary cannot be verified")
}

func inspectionFailure(ctx context.Context, code, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return diagnostics.NewFailure(code, message, "")
}
