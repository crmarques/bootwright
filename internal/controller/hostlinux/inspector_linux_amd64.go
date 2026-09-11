//go:build linux && amd64

// Package hostlinux inspects fixed local Linux evidence without subprocesses,
// network access, package database access, or filesystem mutation.
package hostlinux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"golang.org/x/sys/unix"
)

const (
	maxMetadataBytes = 1 << 20
	maxRuntimeFiles  = 128
	maxRuntimeLinks  = 128
	maxRuntimeBytes  = 512 << 20
	maxRuntimeFile   = 128 << 20
)

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

// Runtime verifies a complete caller-selected file manifest. It never invokes
// Podman or RPM; metadata queries on a native package database may write state.
func (i Inspector) Runtime(ctx context.Context, requirement prerequisites.RuntimeRequirement) (prerequisites.RuntimeInspection, error) {
	requirement.Files = slices.Clone(requirement.Files)
	requirement.Links = slices.Clone(requirement.Links)
	if err := validateRuntimeRequirement(requirement); err != nil {
		return prerequisites.RuntimeInspection{}, inspectionFailure(ctx, "controller.unsupported", "runtime inspection requires a bounded immutable file manifest")
	}
	fs, err := i.view.open(ctx)
	if err != nil {
		return prerequisites.RuntimeInspection{}, inspectionFailure(ctx, "controller.unsupported", "runtime files cannot be inspected safely")
	}
	defer fs.close()
	lock, err := fs.runtimeLock(ctx, requirement.LockPath)
	if err != nil {
		return prerequisites.RuntimeInspection{}, err
	}
	defer lock.Close()
	fs.qualifiedLinks = make(map[string]string, len(requirement.Links))
	for _, link := range requirement.Links {
		fs.qualifiedLinks[link.Path] = link.Target
	}
	primary, stat, err := fs.openPathPolicy(ctx, "/usr/bin/podman", false, false)
	if errors.Is(err, os.ErrNotExist) {
		return prerequisites.RuntimeInspection{}, nil
	}
	if err != nil {
		return prerequisites.RuntimeInspection{Present: true, Conflict: true}, inspectionFailure(ctx, "controller.unsupported", "runtime executable is unsafe or inaccessible")
	}
	primary.Close()
	if !fs.regular(stat) || stat.Mode&0111 == 0 {
		return prerequisites.RuntimeInspection{Present: true, Conflict: true}, nil
	}
	policy, err := fs.selinuxPolicy(ctx, requirement.SELinuxMode)
	if err != nil {
		if ctx.Err() != nil {
			return prerequisites.RuntimeInspection{Present: true}, ctx.Err()
		}
		return prerequisites.RuntimeInspection{Present: true, Conflict: true}, nil
	}
	if policy != nil {
		defer policy.close()
	}
	missing := false
	if err := fs.runtimeLinks(ctx, requirement); err != nil {
		if ctx.Err() != nil {
			return prerequisites.RuntimeInspection{Present: true}, ctx.Err()
		}
		if !errors.Is(err, os.ErrNotExist) {
			return prerequisites.RuntimeInspection{Present: true, Conflict: true}, nil
		}
		missing = true
	}
	var total int64
	for _, required := range requirement.Files {
		file, before, err := fs.openPathPolicy(ctx, required.Path, true, false)
		if err != nil {
			if ctx.Err() != nil {
				return prerequisites.RuntimeInspection{Present: true}, ctx.Err()
			}
			if !errors.Is(err, os.ErrNotExist) {
				return prerequisites.RuntimeInspection{Present: true, Conflict: true}, nil
			}
			missing = true
			continue
		}
		executable := strings.HasPrefix(required.Path, "/usr/bin/") || strings.HasPrefix(required.Path, "/usr/sbin/") || strings.HasPrefix(required.Path, "/usr/libexec/")
		if !fs.regular(before) || executable && before.Mode&0111 == 0 || before.Size < 0 || before.Size > maxRuntimeFile || total > maxRuntimeBytes-before.Size {
			file.Close()
			return prerequisites.RuntimeInspection{Present: true, Conflict: true}, nil
		}
		hash := sha256.New()
		n, readErr := io.Copy(hash, io.LimitReader(contextReader{ctx: ctx, reader: file}, before.Size+1))
		var after unix.Stat_t
		statErr := unix.Fstat(int(file.Fd()), &after)
		file.Close()
		if ctx.Err() != nil {
			return prerequisites.RuntimeInspection{Present: true}, ctx.Err()
		}
		if readErr != nil || statErr != nil || n != before.Size || !stable(before, after) || hex.EncodeToString(hash.Sum(nil)) != required.SHA256 {
			return prerequisites.RuntimeInspection{Present: true, Conflict: true}, nil
		}
		total += n
	}
	if policy != nil && policy.verify(ctx) != nil {
		if ctx.Err() != nil {
			return prerequisites.RuntimeInspection{Present: true}, ctx.Err()
		}
		return prerequisites.RuntimeInspection{Present: true, Conflict: true}, nil
	}
	return prerequisites.RuntimeInspection{Present: true, Ready: !missing}, nil
}

func validateRuntimeRequirement(requirement prerequisites.RuntimeRequirement) error {
	if requirement.Version == "" || len(requirement.Files) == 0 || len(requirement.Files) > maxRuntimeFiles || len(requirement.Links) > maxRuntimeLinks {
		return errEvidence
	}
	if requirement.LockPath != "/usr/lib/sysimage/rpm/.rpm.lock" && requirement.LockPath != "/var/lib/rpm/.rpm.lock" {
		return errEvidence
	}
	if requirement.SELinuxMode != "" && requirement.SELinuxMode != "enforcing" {
		return errEvidence
	}
	seen := map[string]bool{}
	for _, file := range requirement.Files {
		if !runtimeDataPath(file.Path) || seen[file.Path] || len(file.SHA256) != 64 {
			return errEvidence
		}
		digest, err := hex.DecodeString(file.SHA256)
		if err != nil || hex.EncodeToString(digest) != file.SHA256 {
			return errEvidence
		}
		seen[file.Path] = true
	}
	if !seen["/usr/bin/podman"] {
		return errEvidence
	}
	for _, link := range requirement.Links {
		if !runtimeAliasPath(link.Path) || seen[link.Path] || !runtimeTargetPath(link.Path, link.Target) {
			return errEvidence
		}
		seen[link.Path] = true
	}
	return nil
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
	return desiredstate.NewFailure(code, message, "")
}
