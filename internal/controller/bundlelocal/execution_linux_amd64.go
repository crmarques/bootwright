//go:build linux && amd64

package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"golang.org/x/sys/unix"
)

// ExecutionGuard verifies the provided ELF foundation before any private
// Python process can run. Its zero value fixes the installed host root and
// owner; the private view exists only for synthetic filesystem tests. Builds,
// when set, lets an inspection qualify vendor-signed builds within the
// qualified minor from the RPM database; no launch ever reads it.
type ExecutionGuard struct {
	Builds prerequisites.FoundationBuildReader
	view   executionView
}

type executionView struct {
	root  string
	owner uint32
	// packages attributes a synthetic foundation's files in tests, and
	// platform names its release; unset, both come from the compiled record
	// the requirement matches.
	packages []foundationPackage
	platform prerequisites.Platform
}

var _ prerequisites.FoundationInspector = ExecutionGuard{}

// Inspect verifies the provided execution foundation this executable was
// compiled against for platform, under the native package read lock, exactly
// as every private Python launch does, and names the package builds that
// provide it. A foundation that differs is reported with its refusal; the
// error is reserved for an inspection that could not run.
func (guard ExecutionGuard) Inspect(ctx context.Context, platform prerequisites.Platform) (prerequisites.FoundationInspection, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.FoundationInspection{}, err
	}
	record, err := compiledCatalog()
	if err != nil {
		return prerequisites.FoundationInspection{}, err
	}
	native, found := selectNative(record, platform)
	if !found || platform.Architecture != "amd64" {
		return prerequisites.FoundationInspection{}, unsupportedPlatform()
	}
	return guard.inspectRecord(ctx, native)
}

func (guard ExecutionGuard) inspectRecord(ctx context.Context, native nativeRecord) (prerequisites.FoundationInspection, error) {
	requirement := cloneExecution(native.Execution)
	if !validExecutionRequirement(requirement) {
		return prerequisites.FoundationInspection{}, executionFailure("controller.unsupported", "the private Python execution foundation is incomplete")
	}
	view := guard.view
	if view.root == "" {
		view = executionView{root: "/", owner: 0}
	}
	if view.packages == nil {
		view.packages = native.Packages
	}
	root, err := openExecutionRoot(view)
	if err != nil {
		return prerequisites.FoundationInspection{}, executionFailure("controller.unsupported", "the execution foundation root is unsafe")
	}
	defer root.Close()
	fs := executionFilesystem{root: root, owner: view.owner, links: make(map[string]string, len(requirement.Links))}
	for _, link := range requirement.Links {
		fs.links[link.Path] = link.Target
	}
	lock, err := fs.readLock(ctx, requirement.LockPath)
	if err != nil {
		return prerequisites.FoundationInspection{}, err
	}
	defer lock.Close()
	inspection := prerequisites.FoundationInspection{Required: foundationBuilds(view.packages)}
	if err := fs.verify(ctx, requirement); err != nil {
		if ctx.Err() != nil {
			return prerequisites.FoundationInspection{}, ctx.Err()
		}
		drift := driftOf(err)
		inspection.Drift = drift.path
		if guard.Builds == nil || !qualifiable(view, requirement, drift) {
			inspection.Refusal = foundationRefusal(view, requirement, err, guard.Builds == nil)
			return inspection, nil
		}
		platform := prerequisites.Platform{OS: native.OS, Release: native.Release, Architecture: "amd64"}
		qualified, refusal, err := guard.qualify(ctx, platform, root, view, requirement, drift)
		if err != nil {
			return prerequisites.FoundationInspection{}, err
		}
		if refusal != nil {
			inspection.Refusal = refusal
			return inspection, nil
		}
		inspection.Drift, inspection.Qualified = "", qualified
	}
	return inspection, nil
}

func (guard ExecutionGuard) WithPython(ctx context.Context, area prerequisites.BundleArea, requirement prerequisites.ExecutionRequirement, use func(prerequisites.PythonLaunch, func() error) error) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	requirement.Files = slices.Clone(requirement.Files)
	requirement.Links = slices.Clone(requirement.Links)
	requirement.Preload = slices.Clone(requirement.Preload)
	if area == nil || use == nil || !validExecutionRequirement(requirement) {
		return executionFailure("controller.unsupported", "the private Python execution foundation is incomplete")
	}
	view := guard.view
	if view.root == "" {
		view = executionView{root: "/", owner: 0}
	}
	root, err := openExecutionRoot(view)
	if err != nil {
		return executionFailure("controller.unsupported", "the execution foundation root is unsafe")
	}
	defer root.Close()
	fs := executionFilesystem{root: root, owner: view.owner, links: make(map[string]string, len(requirement.Links))}
	for _, link := range requirement.Links {
		fs.links[link.Path] = link.Target
	}
	lock, err := fs.readLock(ctx, requirement.LockPath)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	active := true
	release := func() error {
		mu.Lock()
		defer mu.Unlock()
		if !active {
			return executionFailure("controller.state", "the dependency execution capability has expired")
		}
		if lock == nil {
			return nil
		}
		err := lock.Close()
		lock = nil
		if err != nil {
			return executionFailure("controller.unknown", "the native package read lock could not be released")
		}
		return nil
	}
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		active = false
		if lock != nil {
			if err := lock.Close(); err != nil && result == nil {
				result = executionFailure("controller.unknown", "the native package read lock could not be released")
			}
		}
	}()
	if err := fs.verify(ctx, requirement); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return foundationRefusal(view, requirement, err, true)
	}
	launch, bundle, err := openBundleLaunch(ctx, area, requirement, view.owner)
	if err != nil {
		return err
	}
	defer bundle.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := use(launch, release); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return area.Verify(ctx)
}

func (fs executionFilesystem) readLock(ctx context.Context, name string) (*os.File, error) {
	lock, _, err := fs.open(ctx, name, true, false)
	if err != nil {
		return nil, executionFailure("controller.unsupported", "the qualified native package lock is missing or unsafe")
	}
	readLock := unix.Flock_t{Type: unix.F_RDLCK, Whence: 0, Start: 0, Len: 0}
	if err := unix.FcntlFlock(lock.Fd(), unix.F_OFD_SETLK, &readLock); err != nil {
		lock.Close()
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EAGAIN) {
			return nil, executionFailure("controller.conflict", "a native package transaction prevents coherent dependency execution")
		}
		return nil, executionFailure("controller.unsupported", "the native package lock cannot protect dependency execution")
	}
	return lock, nil
}

func openBundleLaunch(ctx context.Context, area prerequisites.BundleArea, requirement prerequisites.ExecutionRequirement, owner uint32) (prerequisites.PythonLaunch, *os.File, error) {
	if err := area.Verify(ctx); err != nil {
		return prerequisites.PythonLaunch{}, nil, err
	}
	location, err := area.Location(ctx)
	if err != nil {
		return prerequisites.PythonLaunch{}, nil, err
	}
	if !executionPath(location.Path) || location.Device == 0 || location.Inode == 0 {
		return prerequisites.PythonLaunch{}, nil, executionFailure("controller.identity", "the private dependency bundle location is unverified")
	}
	bundle, err := openExecutionRoot(executionView{root: location.Path, owner: owner})
	if err != nil {
		return prerequisites.PythonLaunch{}, nil, executionFailure("controller.identity", "the private dependency bundle execution directory is unsafe")
	}
	var stat unix.Stat_t
	if unix.Fstat(int(bundle.Fd()), &stat) != nil || uint64(stat.Dev) != location.Device || stat.Ino != location.Inode {
		bundle.Close()
		return prerequisites.PythonLaunch{}, nil, executionFailure("controller.identity", "the private dependency bundle execution directory was replaced")
	}
	launch := prerequisites.PythonLaunch{
		Loader: requirement.Loader,
		Arguments: []string{
			"--inhibit-cache", "--glibc-hwcaps-mask", "",
			"--library-path", filepath.Join(location.Path, "python/lib"),
			"--preload", strings.Join(requirement.Preload, ":"),
			filepath.Join(location.Path, executionPython(requirement)),
		},
		Directory: location.Path,
		Environment: []string{
			"LC_ALL=C.UTF-8", "LANG=C.UTF-8", "HOME=" + location.Path, "OPENSSL_CONF=/dev/null",
		},
	}
	return launch, bundle, nil
}

func validExecutionRequirement(value prerequisites.ExecutionRequirement) bool {
	if value.PythonExecutable != "" && !validPythonExecutable(value.PythonExecutable) {
		return false
	}
	if value.Loader != "/usr/lib64/ld-linux-x86-64.so.2" || value.LockPath != "/usr/lib/sysimage/rpm/.rpm.lock" && value.LockPath != "/var/lib/rpm/.rpm.lock" || len(value.Files) < 2 || len(value.Files) > 32 || len(value.Links) > 64 || len(value.Preload) != len(value.Files)-1 {
		return false
	}
	files := make(map[string]bool, len(value.Files))
	for _, file := range value.Files {
		decoded, err := hex.DecodeString(file.SHA256)
		if !executionLibraryPath(file.Path) || files[file.Path] || err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != file.SHA256 {
			return false
		}
		files[file.Path] = true
	}
	if !files[value.Loader] {
		return false
	}
	links := make(map[string]bool, len(value.Links))
	for _, link := range value.Links {
		if !executionPath(link.Path) || link.Path != "/lib64" && !executionLibraryPath(link.Path) || !executionLinkTarget(link.Path, link.Target) || links[link.Path] || files[link.Path] {
			return false
		}
		links[link.Path] = true
	}
	preload := make(map[string]bool, len(value.Preload))
	for _, name := range value.Preload {
		if !files[name] || name == value.Loader || preload[name] {
			return false
		}
		preload[name] = true
	}
	return true
}

func executionPython(value prerequisites.ExecutionRequirement) string {
	if value.PythonExecutable != "" {
		return value.PythonExecutable
	}
	return "python/bin/python3.13"
}

func validPythonExecutable(value string) bool {
	if !strings.HasPrefix(value, "python/bin/python3.") || len(value) > 24 {
		return false
	}
	minor := strings.TrimPrefix(value, "python/bin/python3.")
	if minor == "" {
		return false
	}
	for _, c := range minor {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func executionPath(name string) bool {
	return strings.HasPrefix(name, "/") && name != "/" && path.Clean(name) == name && len(name) <= 4096 && !strings.ContainsAny(name, "\x00\r\n\t:;$")
}

func executionLibraryPath(name string) bool {
	return executionPath(name) && strings.HasPrefix(name, "/usr/lib64/")
}

func executionLinkTarget(name, target string) bool {
	if target == "" || len(target) > 4096 || strings.ContainsAny(target, "\x00\r\n\t :") {
		return false
	}
	destination := target
	if !path.IsAbs(destination) {
		destination = path.Join(path.Dir(name), destination)
	}
	return destination == "/usr/lib64" || executionLibraryPath(destination)
}

func openExecutionRoot(view executionView) (*os.File, error) {
	fd, err := unix.Open(view.root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != view.owner || stat.Mode&0022 != 0 {
		unix.Close(fd)
		return nil, unix.EPERM
	}
	return os.NewFile(uintptr(fd), view.root), nil
}

type executionFilesystem struct {
	root  *os.File
	owner uint32
	links map[string]string
}

func (fs executionFilesystem) open(ctx context.Context, name string, readable, followFinal bool) (*os.File, unix.Stat_t, error) {
	if !executionPath(name) {
		return nil, unix.Stat_t{}, unix.EINVAL
	}
	pending := strings.Split(strings.TrimPrefix(name, "/"), "/")
	resolved := []string{}
	parent, err := unix.Dup(int(fs.root.Fd()))
	if err != nil {
		return nil, unix.Stat_t{}, err
	}
	defer func() { unix.Close(parent) }()
	links := 0
	for len(pending) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, unix.Stat_t{}, err
		}
		component := pending[0]
		pending = pending[1:]
		fd, err := unix.Openat(parent, component, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, unix.Stat_t{}, err
		}
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil || stat.Uid != fs.owner {
			unix.Close(fd)
			return nil, unix.Stat_t{}, unix.EPERM
		}
		current := "/" + strings.Join(append(slices.Clone(resolved), component), "/")
		if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
			if len(pending) == 0 && !followFinal {
				if !readable {
					return os.NewFile(uintptr(fd), name), stat, nil
				}
				unix.Close(fd)
				return nil, unix.Stat_t{}, unix.ELOOP
			}
			buffer := make([]byte, 4097)
			n, err := unix.Readlinkat(fd, "", buffer)
			unix.Close(fd)
			links++
			if err != nil || n == 0 || n > 4096 || links > 16 || fs.links[current] != string(buffer[:n]) {
				return nil, unix.Stat_t{}, unix.ELOOP
			}
			target := string(buffer[:n])
			if !path.IsAbs(target) {
				target = path.Join(path.Dir(current), target)
			}
			pending = append(strings.Split(strings.TrimPrefix(path.Clean(target), "/"), "/"), pending...)
			resolved = nil
			unix.Close(parent)
			parent, err = unix.Dup(int(fs.root.Fd()))
			if err != nil {
				return nil, unix.Stat_t{}, err
			}
			continue
		}
		if len(pending) != 0 {
			if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0022 != 0 {
				unix.Close(fd)
				return nil, unix.Stat_t{}, unix.EPERM
			}
			unix.Close(parent)
			parent = fd
			resolved = append(resolved, component)
			continue
		}
		if !readable {
			return os.NewFile(uintptr(fd), name), stat, nil
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0022 != 0 || stat.Nlink != 1 {
			unix.Close(fd)
			return nil, unix.Stat_t{}, unix.EPERM
		}
		readFD, err := unix.Openat(parent, component, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOATIME|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if err != nil {
			return nil, unix.Stat_t{}, err
		}
		var opened unix.Stat_t
		if unix.Fstat(readFD, &opened) != nil || !sameExecutionFile(stat, opened) {
			unix.Close(readFD)
			return nil, unix.Stat_t{}, unix.ESTALE
		}
		return os.NewFile(uintptr(readFD), name), opened, nil
	}
	return nil, unix.Stat_t{}, unix.EINVAL
}

func sameExecutionFile(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino && before.Mode == after.Mode && before.Uid == after.Uid && before.Gid == after.Gid && before.Nlink == after.Nlink && before.Size == after.Size && before.Mtim == after.Mtim && before.Ctim == after.Ctim
}

// verify returns the first place the host's foundation differs from the
// requirement as a *foundationDrift, or the context's error.
func (fs executionFilesystem) verify(ctx context.Context, requirement prerequisites.ExecutionRequirement) error {
	preload, stat, err := fs.open(ctx, preloadConfiguration, true, false)
	if preload != nil {
		preload.Close()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return &foundationDrift{path: preloadConfiguration, condition: driftUnsafe}
	}
	if err == nil && stat.Size != 0 {
		return &foundationDrift{path: preloadConfiguration, condition: driftPreload}
	}
	files := make(map[string]bool, len(requirement.Files))
	var total int64
	for _, approved := range requirement.Files {
		file, before, err := fs.open(ctx, approved.Path, true, false)
		if err != nil {
			return openDrift(ctx, approved.Path, err)
		}
		if before.Size <= 0 || before.Size > 128<<20 || total > 512<<20-before.Size {
			file.Close()
			return &foundationDrift{path: approved.Path, condition: driftContent}
		}
		total += before.Size
		digest := sha256.New()
		_, err = io.Copy(digest, io.LimitReader(executionReader{ctx, file}, before.Size+1))
		var after unix.Stat_t
		statErr := unix.Fstat(int(file.Fd()), &after)
		file.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil || statErr != nil || !sameExecutionFile(before, after) {
			return &foundationDrift{path: approved.Path, condition: driftChanged}
		}
		if hex.EncodeToString(digest.Sum(nil)) != approved.SHA256 {
			return &foundationDrift{path: approved.Path, condition: driftContent}
		}
		files[approved.Path] = true
	}
	for _, approved := range requirement.Links {
		file, before, err := fs.open(ctx, approved.Path, false, false)
		if err != nil {
			return openDrift(ctx, approved.Path, err)
		}
		buffer := make([]byte, 4097)
		n, readErr := unix.Readlinkat(int(file.Fd()), "", buffer)
		var after unix.Stat_t
		statErr := unix.Fstat(int(file.Fd()), &after)
		file.Close()
		if before.Mode&unix.S_IFMT != unix.S_IFLNK {
			return &foundationDrift{path: approved.Path, condition: driftNotLink}
		}
		if readErr != nil || statErr != nil || n == 0 || n > 4096 || !sameExecutionFile(before, after) {
			return &foundationDrift{path: approved.Path, condition: driftChanged}
		}
		if string(buffer[:n]) != approved.Target {
			return &foundationDrift{path: approved.Path, condition: "points elsewhere than " + approved.Target}
		}
		destination := approved.Target
		if !path.IsAbs(destination) {
			destination = path.Join(path.Dir(approved.Path), destination)
		}
		for count := 0; fs.links[destination] != ""; count++ {
			if count == 16 {
				return &foundationDrift{path: approved.Path, condition: driftUnpinnedLink}
			}
			target := fs.links[destination]
			if !path.IsAbs(target) {
				target = path.Join(path.Dir(destination), target)
			}
			destination = target
		}
		if !files[destination] && destination != "/usr/lib64" {
			return &foundationDrift{path: approved.Path, condition: driftUnpinnedLink}
		}
	}
	return ctx.Err()
}

const preloadConfiguration = "/etc/ld.so.preload"

// The conditions a foundation path can be found in, as a refusal states them
// after the path.
const (
	driftMissing      = "is missing"
	driftContent      = "holds other content than this build pins"
	driftChanged      = "changed while it was verified"
	driftUnsafe       = "has an unsafe owner, mode, type or link count"
	driftUnpinnedLink = "is reached through a link this build does not pin"
	driftNotLink      = "is no longer a symbolic link"
	driftPreload      = "is not empty, so it would preload libraries into every process"
)

// foundationDrift is the first path at which the host's execution foundation
// differs from the one a requirement pins, and what was found there.
type foundationDrift struct {
	path, condition string
}

func (drift *foundationDrift) Error() string { return drift.path + " " + drift.condition }

// openDrift names why a pinned path could not be opened as the foundation
// requires.
func openDrift(ctx context.Context, name string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	switch {
	case errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENOTDIR):
		return &foundationDrift{path: name, condition: driftMissing}
	case errors.Is(err, unix.ELOOP):
		return &foundationDrift{path: name, condition: driftUnpinnedLink}
	case errors.Is(err, unix.ESTALE):
		return &foundationDrift{path: name, condition: driftChanged}
	}
	return &foundationDrift{path: name, condition: driftUnsafe}
}

// driftOf is the drift a verification returned, or one that names nothing
// when it returned another error.
func driftOf(err error) foundationDrift {
	var drift *foundationDrift
	if errors.As(err, &drift) {
		return *drift
	}
	return foundationDrift{condition: "cannot be verified"}
}

// foundationRefusal is the one refusal of a foundation that drifted, whichever
// command met it: the private Python guard before any launch, or the check
// setup and preflight report. It names the path, the package build that
// provides it on a qualified host, the condition, and a remedy that holds
// from every command. Where setup can still qualify an update of that build,
// the remedy first names setup.
func foundationRefusal(view executionView, requirement prerequisites.ExecutionRequirement, err error, qualifiable bool) error {
	drift := driftOf(err)
	if drift.path == "" {
		return executionFailure("controller.unsupported", "the provided execution foundation cannot be verified")
	}
	message := "the provided execution foundation differs at " + drift.path + ","
	pkg, attributed := foundationOwner(view, requirement, drift.path)
	if attributed {
		message += " from " + pkg.described() + ","
	}
	message += " which " + drift.condition
	remedy := "Restore " + drift.path + " as this host's release provides it, then repeat this command; a host that must keep the change needs a Bootwright build whose execution foundation pins it."
	switch {
	case drift.path == preloadConfiguration:
		remedy = "Empty or remove " + preloadConfiguration + ", then repeat this command."
	case attributed:
		remedy = foundationRemedy(pkg)
		if platform, known := foundationPlatform(view, requirement); qualifiable && known {
			remedy = "If dnf updated glibc or libgcc within " + platform.OS + " " + platform.Release + ", run bootwright setup to qualify the new builds; otherwise " + strings.ToLower(remedy[:1]) + remedy[1:]
		}
	}
	return diagnostics.NewFailureWithRemediation("controller.unsupported", message, "", remedy)
}

// foundationPlatform is the release whose foundation a requirement pins.
func foundationPlatform(view executionView, requirement prerequisites.ExecutionRequirement) (prerequisites.Platform, bool) {
	if view.platform.OS != "" {
		return view.platform, true
	}
	native, _, found := compiledNativeFor(requirement)
	return prerequisites.Platform{OS: native.OS, Release: native.Release, Architecture: "amd64"}, found
}

// foundationRemedy reinstalls and holds the package build that provides a
// path, or, for a build only setup's qualification names, that package's
// build as setup qualified it.
func foundationRemedy(pkg foundationPackage) string {
	if pkg.Build == "" {
		return "Install the " + pkg.Name + " build setup qualified again with dnf (dnf reinstall " + pkg.Name + "), hold it with dnf versionlock add " + pkg.Name + ", then repeat this command; a host that must take another build needs a Bootwright build whose execution foundation pins it."
	}
	build := pkg.Name + "-" + pkg.Build
	return "Install exactly " + pkg.Name + " " + pkg.Build + " again with dnf (dnf install " + build + ", or dnf reinstall " + build + " while that build is installed), hold it with dnf versionlock add " + build + ", then repeat this command; a host that must take the update needs a Bootwright build whose execution foundation pins it."
}

// foundationOwner is the package build that provides a foundation path: the
// one listing it, or for a pinned link the one listing the file it resolves
// to.
func foundationOwner(view executionView, requirement prerequisites.ExecutionRequirement, name string) (foundationPackage, bool) {
	packages := view.packages
	if packages == nil {
		packages = compiledAttribution(requirement)
	}
	links := make(map[string]string, len(requirement.Links))
	for _, link := range requirement.Links {
		links[link.Path] = link.Target
	}
	for count := 0; links[name] != "" && count < 16; count++ {
		target := links[name]
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(name), target)
		}
		name = target
	}
	for _, pkg := range packages {
		if slices.Contains(pkg.Files, name) {
			return pkg, true
		}
	}
	return foundationPackage{}, false
}

type executionReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader executionReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

// executionFailure is scoped: setup, preflight, apply, destroy and a power run
// each meet the foundation, so an unscoped reading repeats the command that
// met it, and a context's controller stage names its own.
func executionFailure(code, message string) error {
	return &prerequisites.ScopedFailure{Code: code, Message: message, Correction: "Restore the qualified host execution foundation", Command: "repeat this command"}
}

// qualifiable reports a drift that an update of the package build providing
// the path could explain: other content, a missing file or a link that points
// elsewhere, at a path a foundation package provides. An unsafe mode, a race
// or a preload is never a package update.
func qualifiable(view executionView, requirement prerequisites.ExecutionRequirement, drift foundationDrift) bool {
	if drift.path == "" || drift.path == preloadConfiguration || drift.condition == driftUnsafe || drift.condition == driftChanged {
		return false
	}
	_, attributed := foundationOwner(view, requirement, drift.path)
	return attributed
}

// qualify proves, from the RPM database, the foundation the host's installed
// builds provide in the compiled requirement's shape, and verifies it byte for
// byte under the read lock the caller holds. Each build must be the one
// x86_64 instance of its package, of the compiled upstream version, signed by
// the platform's vendor key and recording SHA-256 file digests; each pinned
// file takes that build's digest, and the versioned libgcc file, its link and
// its preload entry take that build's file and link target. Its refusal names
// the package build and what disqualified it; the error is reserved for a
// reading that could not run.
func (guard ExecutionGuard) qualify(ctx context.Context, platform prerequisites.Platform, root *os.File, view executionView, compiled prerequisites.ExecutionRequirement, drift foundationDrift) (*prerequisites.QualifiedFoundation, error, error) {
	owner, _ := foundationOwner(view, compiled, drift.path)
	refuse := func(reason string, remedied foundationPackage) error {
		message := "the provided execution foundation differs at " + drift.path + ", from " + owner.described() + ", which " + drift.condition + ", and " + reason
		return diagnostics.NewFailureWithRemediation("controller.unsupported", message, "", foundationRemedy(remedied))
	}
	names := make([]string, 0, len(view.packages))
	for _, pkg := range view.packages {
		names = append(names, pkg.Name)
	}
	installed, err := guard.Builds.FoundationBuilds(ctx, platform, names)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, err
	}
	proof := foundationProof{renamed: map[string]string{}, digests: map[string]string{}, provided: map[string]map[string]prerequisites.InstalledBuildFile{}}
	qualified := &prerequisites.QualifiedFoundation{}
	for _, pkg := range view.packages {
		record, files, reason := qualifyBuild(pkg, installed, platform)
		if reason != "" {
			return nil, refuse(reason, pkg), nil
		}
		proof.provided[pkg.Name] = files
		for index, name := range pkg.Files {
			proof.renamed[name], proof.digests[record.Files[index]] = record.Files[index], files[record.Files[index]].SHA256
		}
		qualified.Packages = append(qualified.Packages, record)
	}
	var reason string
	var remedied foundationPackage
	qualified.Execution, reason, remedied = proof.requirement(view, compiled, qualified.Packages)
	if reason != "" {
		return nil, refuse(reason, remedied), nil
	}
	unpythoned := compiled
	unpythoned.PythonExecutable = ""
	if !validExecutionRequirement(qualified.Execution) || prerequisites.ValidateQualifiedFoundation(*qualified, unpythoned) != nil {
		return nil, refuse("the installed builds do not provide a foundation in the shape this build pins", owner), nil
	}
	refusal, err := verifyQualified(ctx, root, view.owner, qualified)
	if err != nil || refusal != nil {
		return nil, refusal, err
	}
	return qualified, nil, nil
}

// qualifyBuild selects the one x86_64 instance of a pinned package and admits
// it only at the compiled upstream version, signed by the vendor key and with
// SHA-256 file digests, returning the files it provides under the pinned
// paths, or the reason it does not qualify.
func qualifyBuild(pkg foundationPackage, installed []prerequisites.InstalledBuild, platform prerequisites.Platform) (prerequisites.FoundationBuild, map[string]prerequisites.InstalledBuildFile, string) {
	var instances []prerequisites.InstalledBuild
	for _, build := range installed {
		if build.Name == pkg.Name && build.Architecture == "x86_64" {
			instances = append(instances, build)
		}
	}
	switch {
	case len(instances) == 0:
		return prerequisites.FoundationBuild{}, nil, pkg.Name + " has no x86_64 instance installed"
	case len(instances) > 1:
		return prerequisites.FoundationBuild{}, nil, "more than one x86_64 instance of " + pkg.Name + " is installed"
	}
	build := instances[0]
	record := prerequisites.FoundationBuild{Name: pkg.Name, Build: build.Version + "-" + build.Release, Files: []string{}}
	described := pkg.Name + " " + record.Build
	switch {
	case build.Epoch != 0 || build.Version != pkg.upstreamVersion():
		return record, nil, described + " is not upstream version " + pkg.upstreamVersion() + ", which this build pins"
	case !build.Signed:
		return record, nil, described + " is not signed by the " + platform.OS + " vendor key"
	case build.DigestAlgorithm != 8:
		return record, nil, described + " does not record SHA-256 file digests"
	}
	files, consistent := usrMergedFiles(build.Files)
	if !consistent {
		return record, nil, described + " lists one foundation path twice"
	}
	for _, name := range pkg.Files {
		found := name
		if prerequisites.VersionedLibgcc(name) {
			found = ""
			for candidate, file := range files {
				if prerequisites.VersionedLibgcc(candidate) && file.LinkTo == "" && file.SHA256 != "" {
					if found != "" {
						return record, nil, described + " provides more than one versioned libgcc_s library"
					}
					found = candidate
				}
			}
		}
		if file, listed := files[found]; found == "" || !listed || file.LinkTo != "" || file.SHA256 == "" {
			return record, nil, described + " does not provide " + name + " as a file"
		}
		record.Files = append(record.Files, found)
	}
	return record, files, ""
}

// foundationProof is what the qualified builds proved: where each pinned file
// stands now, the digest each qualified file must hold and every file each
// package provides.
type foundationProof struct {
	renamed, digests map[string]string
	provided         map[string]map[string]prerequisites.InstalledBuildFile
}

// requirement is the compiled requirement under the proved files: each file
// at its qualified path with its RPM digest, each link to a moved file at the
// target its package records, and each preload entry renamed. It returns the
// reason and the package to reinstall when a package does not link a moved
// file as the compiled requirement does.
func (proof foundationProof) requirement(view executionView, compiled prerequisites.ExecutionRequirement, builds []prerequisites.FoundationBuild) (prerequisites.ExecutionRequirement, string, foundationPackage) {
	requirement := prerequisites.ExecutionRequirement{Loader: compiled.Loader, LockPath: compiled.LockPath}
	for _, file := range compiled.Files {
		name := proof.renamed[file.Path]
		requirement.Files = append(requirement.Files, prerequisites.InstalledFile{Path: name, SHA256: proof.digests[name]})
	}
	for _, link := range compiled.Links {
		destination := linkTarget(link)
		moved, renames := proof.renamed[destination]
		if !renames || moved == destination {
			requirement.Links = append(requirement.Links, link)
			continue
		}
		pkg, _ := foundationOwner(view, compiled, destination)
		listed, found := proof.provided[pkg.Name][link.Path]
		if !found || listed.LinkTo == "" || linkTarget(prerequisites.InstalledLink{Path: link.Path, Target: listed.LinkTo}) != moved {
			build := ""
			for _, record := range builds {
				if record.Name == pkg.Name {
					build = record.Build
				}
			}
			return requirement, pkg.Name + " " + build + " does not link " + link.Path + " to " + path.Base(moved), pkg
		}
		requirement.Links = append(requirement.Links, prerequisites.InstalledLink{Path: link.Path, Target: listed.LinkTo})
	}
	for _, name := range compiled.Preload {
		requirement.Preload = append(requirement.Preload, proof.renamed[name])
	}
	return requirement, "", foundationPackage{}
}

// verifyQualified verifies a qualified foundation byte for byte, exactly as a
// launch would, and refuses a path that differs naming the qualified build
// that provides it.
func verifyQualified(ctx context.Context, root *os.File, owner uint32, qualified *prerequisites.QualifiedFoundation) (error, error) {
	fs := executionFilesystem{root: root, owner: owner, links: make(map[string]string, len(qualified.Execution.Links))}
	for _, link := range qualified.Execution.Links {
		fs.links[link.Path] = link.Target
	}
	err := fs.verify(ctx, qualified.Execution)
	if err == nil {
		return nil, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	found := driftOf(err)
	qualifiedView := executionView{packages: qualifiedPackages(qualified.Packages)}
	pkg, attributed := foundationOwner(qualifiedView, qualified.Execution, found.path)
	if !attributed {
		return foundationRefusal(qualifiedView, qualified.Execution, err, false), nil
	}
	condition := found.condition
	if condition == driftContent {
		condition = "holds other content than the RPM database records for that build"
	}
	message := "the provided execution foundation differs at " + found.path + ", from " + pkg.described() + ", which " + condition
	return diagnostics.NewFailureWithRemediation("controller.unsupported", message, "", foundationRemedy(pkg)), nil
}

// usrMergedFiles is a build's file list under the paths a foundation pins:
// rpm lists a file under /lib64 where the package installs it, which the
// pinned /lib64 link makes /usr/lib64. A path listed both ways is refused.
func usrMergedFiles(files map[string]prerequisites.InstalledBuildFile) (map[string]prerequisites.InstalledBuildFile, bool) {
	merged := make(map[string]prerequisites.InstalledBuildFile, len(files))
	for name, file := range files {
		if rest, found := strings.CutPrefix(name, "/lib64/"); found {
			name = "/usr/lib64/" + rest
		}
		if _, duplicate := merged[name]; duplicate {
			return nil, false
		}
		merged[name] = file
	}
	return merged, true
}

func qualifiedPackages(builds []prerequisites.FoundationBuild) []foundationPackage {
	packages := make([]foundationPackage, 0, len(builds))
	for _, build := range builds {
		packages = append(packages, foundationPackage{Name: build.Name, Build: build.Build, Files: slices.Clone(build.Files)})
	}
	return packages
}

func linkTarget(link prerequisites.InstalledLink) string {
	if path.IsAbs(link.Target) {
		return path.Clean(link.Target)
	}
	return path.Join(path.Dir(link.Path), link.Target)
}
