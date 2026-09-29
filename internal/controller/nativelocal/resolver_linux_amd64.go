//go:build linux && amd64

// Package nativelocal runs the provided OS package solver without host mutations.
package nativelocal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"golang.org/x/sys/unix"
)

type MetadataReader func(context.Context, string, string, int64, prerequisites.SetupEgress) ([]byte, int64, error)

// Resolver retains one database snapshot between operations. Every inspection
// copies the complete installed inventory, and one setup proves presence
// several times, so the copy is repeated only when the provided host's own
// package state has moved. Close releases what it retains.
type Resolver struct {
	metadata MetadataReader
	mu       sync.Mutex
	retained *retainedDatabase
}

type retainedDatabase struct {
	parent   string
	root     string
	platform prerequisites.Platform
	identity databaseIdentity
}

// databaseIdentity is what the live database must still look like for a
// snapshot of it to remain usable. A package transaction moves the size and
// both timestamps, so a stale snapshot can never be mistaken for a current one.
type databaseIdentity struct {
	Files [2]fileIdentity
}

type fileIdentity struct {
	Present           bool
	Device, Inode     uint64
	Size              int64
	Modified, Changed int64
}

// serves reports whether this snapshot still describes the installed state a
// caller is asking about. Anything that moved the database — a completed
// transaction, a rebuilt or replaced file — makes it describe the past instead.
func (d *retainedDatabase) serves(platform prerequisites.Platform, identity databaseIdentity) bool {
	return d != nil && d.platform == platform && d.identity == identity
}

const unprivilegedID = 65534

var databaseMembers = [2]string{"rpmdb.sqlite", "rpmdb.sqlite-wal"}

func New(metadata MetadataReader) *Resolver { return &Resolver{metadata: metadata} }

// Close releases the retained database snapshot. It is safe to call on a
// resolver that never took one and to call more than once.
func (r *Resolver) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discard()
}

func (r *Resolver) discard() {
	if r.retained != nil {
		_ = os.RemoveAll(r.retained.parent)
		r.retained = nil
	}
}

var _ prerequisites.NativeResolver = (*Resolver)(nil)
var _ prerequisites.NativeInspector = (*Resolver)(nil)

type repository struct {
	ID             string `json:"id"`
	BaseURL        string `json:"baseURL"`
	MetadataSHA256 string `json:"metadataSHA256"`
	LocalPath      string `json:"localPath"`
	Signer         string `json:"signer"`
}
type helperRequest struct {
	Operation    string                            `json:"operation"`
	Platform     prerequisites.Platform            `json:"platform"`
	Requirements prerequisites.NativeRequirements  `json:"requirements"`
	Versions     controller.DependencyVersions     `json:"versions"`
	Egress       prerequisites.SetupEgress         `json:"egress"`
	Snapshot     string                            `json:"snapshot"`
	Repositories []repository                      `json:"repositories"`
	Plan         *prerequisites.NativeResolvedPlan `json:"plan,omitempty"`
}

func (r *Resolver) Resolve(ctx context.Context, platform prerequisites.Platform, requirements prerequisites.NativeRequirements, versions controller.DependencyVersions, egress prerequisites.SetupEgress) (prerequisites.NativeResolvedPlan, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.NativeResolvedPlan{}, err
	}
	if r == nil || r.metadata == nil {
		return prerequisites.NativeResolvedPlan{}, failure("native metadata resolver is unavailable")
	}
	repositories, err := profiles(platform, requirements)
	if err != nil {
		return prerequisites.NativeResolvedPlan{}, err
	}
	stage, err := r.newStage(platform)
	if err != nil {
		return prerequisites.NativeResolvedPlan{}, err
	}
	defer os.RemoveAll(stage.root)
	bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for index := range repositories {
		if err := r.stageRepository(bounded, stage.work, &repositories[index], egress); err != nil {
			return prerequisites.NativeResolvedPlan{}, err
		}
	}
	request := helperRequest{"resolve", platform, requirements, versions, egress, stage.snapshot, repositories, nil}
	data, err := stage.run(bounded, request)
	if err != nil {
		return prerequisites.NativeResolvedPlan{}, err
	}
	var plan prerequisites.NativeResolvedPlan
	if strictDecode(data, &plan) != nil || prerequisites.ValidateNativePlan(plan) != nil || plan.Platform != platform || plan.Requirements != requirements || plan.Requests != versions {
		return prerequisites.NativeResolvedPlan{}, failure("native solver returned an invalid frozen dependency plan")
	}
	for _, actual := range plan.Repositories {
		found := false
		for _, expected := range repositories {
			found = found || actual.ID == expected.ID && actual.BaseURL == expected.BaseURL && actual.MetadataSHA256 == expected.MetadataSHA256
		}
		if !found {
			return prerequisites.NativeResolvedPlan{}, failure("native solver changed its authenticated repository metadata")
		}
	}
	if len(plan.Repositories) != len(repositories) {
		return prerequisites.NativeResolvedPlan{}, failure("native solver omitted authenticated repository metadata")
	}
	for _, pkg := range plan.Packages {
		found := false
		for _, repo := range repositories {
			found = found || strings.HasPrefix(pkg.Source.URL, repo.BaseURL+"/") && pkg.Signer == repo.Signer
		}
		if !found {
			return prerequisites.NativeResolvedPlan{}, failure("native solver changed its publisher authority")
		}
	}
	return plan, nil
}

func (r *Resolver) Check(ctx context.Context, plan prerequisites.NativeResolvedPlan) (prerequisites.NativePresence, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.NativePresence{}, err
	}
	if r == nil {
		return prerequisites.NativePresence{}, failure("native dependency inspection is unavailable")
	}
	if prerequisites.ValidateNativePlan(plan) != nil {
		return prerequisites.NativePresence{}, failure("native readiness requires an exact frozen plan")
	}
	stage, err := r.newStage(plan.Platform)
	if err != nil {
		return prerequisites.NativePresence{}, err
	}
	defer os.RemoveAll(stage.root)
	bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	data, err := stage.run(bounded, helperRequest{Operation: "present", Platform: plan.Platform, Requirements: plan.Requirements, Versions: plan.Requests, Egress: prerequisites.SetupEgress{NoProxy: []string{}}, Snapshot: stage.snapshot, Repositories: []repository{}, Plan: &plan})
	if err != nil {
		return prerequisites.NativePresence{}, err
	}
	return decodePresence(plan, data)
}

// decodePresence accepts only a report naming every selected root of the plan
// in order. Installed identities are bounded display evidence.
func decodePresence(plan prerequisites.NativeResolvedPlan, data []byte) (prerequisites.NativePresence, error) {
	var result struct {
		Roots []struct {
			Key       string                        `json:"key"`
			Name      string                        `json:"name"`
			Installed *prerequisites.NativeIdentity `json:"installed"`
		} `json:"roots"`
		RootsReady bool `json:"rootsReady"`
	}
	invalid := func() (prerequisites.NativePresence, error) {
		return prerequisites.NativePresence{}, failure("native readiness returned invalid presence evidence")
	}
	if strictDecode(data, &result) != nil || len(result.Roots) != len(plan.Roots) {
		return invalid()
	}
	presence := prerequisites.NativePresence{Ready: result.RootsReady}
	for index, root := range result.Roots {
		expected := plan.Roots[index]
		if root.Key != expected.Key || root.Name != expected.Package.Name {
			return invalid()
		}
		if root.Installed == nil {
			continue
		}
		installed := *root.Installed
		if installed.Name != expected.Package.Name || installed.Version == "" || len(installed.Version) > 128 || installed.Release == "" || len(installed.Release) > 128 || installed.Architecture == "" || len(installed.Architecture) > 128 || installed.Epoch < 0 {
			return invalid()
		}
		presence.Installed = append(presence.Installed, prerequisites.NativeRootPresence{Key: root.Key, Package: installed})
	}
	if presence.Ready != (len(presence.Installed) == len(plan.Roots)) {
		return invalid()
	}
	return presence, nil
}

func profiles(platform prerequisites.Platform, requirements prerequisites.NativeRequirements) ([]repository, error) {
	if platform.Architecture != "amd64" {
		return nil, failure("native dependency resolution requires Linux amd64")
	}
	switch {
	case platform.OS == "fedora" && platform.Release == "43":
		signer := "c6e7f081cf80e13146676e88829b606631645531"
		return []repository{{ID: "fedora", BaseURL: "https://dl.fedoraproject.org/pub/fedora/linux/releases/43/Everything/x86_64/os", Signer: signer}, {ID: "updates", BaseURL: "https://dl.fedoraproject.org/pub/fedora/linux/updates/43/Everything/x86_64", Signer: signer}}, nil
	case platform.OS == "rhel" && platform.Release == "9.8":
		if requirements.LibvirtClient {
			return nil, diagnostics.NewFailureWithRemediation("controller.unsupported", "RHEL libvirt requires an authenticated AppStream source adapter", "", "Use a Fedora controller for libvirt preparation until RHEL AppStream credential acquisition is configured.")
		}
		signer := "567e347ad0044ade55ba8a5f199e2f91fd431d51"
		return []repository{{ID: "ubi-baseos", BaseURL: "https://cdn-ubi.redhat.com/content/public/ubi/dist/ubi9/9/x86_64/baseos/os", Signer: signer}, {ID: "ubi-appstream", BaseURL: "https://cdn-ubi.redhat.com/content/public/ubi/dist/ubi9/9/x86_64/appstream/os", Signer: signer}}, nil
	default:
		return nil, failure("native dependency resolution requires a supported Fedora 43 or RHEL 9.8 package-manager foundation")
	}
}

type repomd struct {
	XMLName xml.Name `xml:"repomd"`
	Data    []struct {
		Type     string `xml:"type,attr"`
		Checksum struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"checksum"`
		Location struct {
			Href string `xml:"href,attr"`
		} `xml:"location"`
		Size int64 `xml:"size"`
	} `xml:"data"`
}

func (r *Resolver) stageRepository(ctx context.Context, work string, repo *repository, egress prerequisites.SetupEgress) error {
	data, _, err := r.metadata(ctx, http.MethodGet, repo.BaseURL+"/repodata/repomd.xml", 1<<20, egress)
	if err != nil {
		return err
	}
	var metadata repomd
	if xml.Unmarshal(data, &metadata) != nil || len(metadata.Data) > 32 {
		return failure("native repository metadata is malformed or excessive")
	}
	digest := sha256.Sum256(data)
	repo.MetadataSHA256 = hex.EncodeToString(digest[:])
	repo.LocalPath = filepath.Join(work, "repositories", repo.ID)
	directory := filepath.Join(repo.LocalPath, "repodata")
	if os.MkdirAll(directory, 0700) != nil || os.WriteFile(filepath.Join(directory, "repomd.xml"), data, 0600) != nil {
		return failure("native repository staging failed")
	}
	selected := map[string]bool{}
	for _, entry := range metadata.Data {
		// The solver decides for itself which of these it opens, so every one it
		// may ask for is staged. Narrowing the set to what a solver is
		// configured to load is not observable from here, and a member it does
		// open and cannot find fails the whole repository, not just that member.
		if entry.Type != "primary" && entry.Type != "filelists" && entry.Type != "modules" && entry.Type != "group" && entry.Type != "group_gz" && entry.Type != "updateinfo" && entry.Type != "other" {
			continue
		}
		relative := entry.Location.Href
		if selected[entry.Type] || entry.Checksum.Type != "sha256" || len(entry.Checksum.Value) != 64 || strings.Trim(entry.Checksum.Value, "0123456789abcdef") != "" || entry.Size <= 0 || entry.Size > 64<<20 || !strings.HasPrefix(relative, "repodata/") || filepath.Clean(relative) != relative || strings.ContainsAny(relative, "\\?#\x00") || strings.Count(relative, "/") != 1 {
			return failure("native repository member exceeds its exact integrity or path bounds")
		}
		member, _, err := r.metadata(ctx, http.MethodGet, repo.BaseURL+"/"+relative, entry.Size, egress)
		if err != nil {
			return err
		}
		actual := sha256.Sum256(member)
		if int64(len(member)) != entry.Size || hex.EncodeToString(actual[:]) != entry.Checksum.Value {
			return failure("native repository member differs from publisher metadata")
		}
		if os.WriteFile(filepath.Join(repo.LocalPath, relative), member, 0600) != nil {
			return failure("native repository staging failed")
		}
		selected[entry.Type] = true
	}
	if !selected["primary"] || !selected["filelists"] {
		return failure("native repository omits required dependency metadata")
	}
	return nil
}

type nativeStage struct {
	root, work, script, interpreter, snapshot string
	uid, gid                                  int
}

// stageDatabase returns a snapshot root the unprivileged helper can read,
// reusing the retained one while the live database still has the identity it
// was copied from. The copy itself remains the only way a solver or query sees
// installed state, so nothing here reads the live database directly.
func (r *Resolver) stageDatabase(platform prerequisites.Platform) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	identity, err := databaseState(platform)
	if err != nil {
		return "", err
	}
	if r.retained.serves(platform, identity) {
		return r.retained.root, nil
	}
	parent, err := os.MkdirTemp("/tmp", "bootwright-rpmdb-")
	if err != nil {
		return "", failure("native database snapshot staging failed")
	}
	root, taken, err := copyDatabase(platform, parent)
	if err == nil {
		err = shareDatabase(parent, root)
	}
	if err != nil {
		_ = os.RemoveAll(parent)
		return "", err
	}
	r.discard()
	r.retained = &retainedDatabase{parent: parent, root: root, platform: platform, identity: taken}
	return root, nil
}

// shareDatabase grants the snapshot to the unprivileged helper identity exactly
// as the rest of the stage is granted, because the snapshot no longer lives
// inside the staging tree that is handed over per operation.
func shareDatabase(parent, root string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if os.Chmod(parent, 0755) != nil {
		return failure("native resolver privilege separation failed")
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("native snapshot symlink")
		}
		return os.Chown(path, unprivilegedID, unprivilegedID)
	})
	if err != nil {
		return failure("native resolver staging privilege separation failed")
	}
	return nil
}

// databaseState reads the live database identity under the same read lock a
// snapshot is taken with, so a transaction cannot complete between the check
// and the decision it settles.
func databaseState(platform prerequisites.Platform) (databaseIdentity, error) {
	directory, err := databaseDirectory(platform)
	if err != nil {
		return databaseIdentity{}, err
	}
	release, err := lockDatabase(directory)
	if err != nil {
		return databaseIdentity{}, err
	}
	defer release()
	var identity databaseIdentity
	for index, name := range databaseMembers {
		var info unix.Stat_t
		if err := unix.Stat(directory+"/"+name, &info); err != nil {
			if errors.Is(err, unix.ENOENT) && strings.HasSuffix(name, "-wal") {
				continue
			}
			return databaseIdentity{}, failure("native SQLite database is unavailable")
		}
		identity.Files[index] = fileIdentity{Present: true, Device: uint64(info.Dev), Inode: info.Ino, Size: info.Size, Modified: info.Mtim.Nano(), Changed: info.Ctim.Nano()}
	}
	return identity, nil
}

func databaseDirectory(platform prerequisites.Platform) (string, error) {
	switch platform.OS {
	case "fedora":
		return "/usr/lib/sysimage/rpm", nil
	case "rhel":
		return "/var/lib/rpm", nil
	}
	return "", failure("native dependency resolution requires a supported Fedora 43 or RHEL 9.8 package-manager foundation")
}

// lockDatabase holds the provided host's own read lock, so no package
// transaction can change the database while it is being identified or copied.
func lockDatabase(directory string) (func(), error) {
	descriptor, err := unix.Open(directory+"/.rpm.lock", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, failure("native database read lock is unavailable")
	}
	var info unix.Stat_t
	if unix.Fstat(descriptor, &info) != nil || info.Uid != 0 || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0022 != 0 {
		unix.Close(descriptor)
		return nil, failure("native database lock lacks provided-host authority")
	}
	lock := unix.Flock_t{Type: unix.F_RDLCK, Whence: 0, Start: 0, Len: 0}
	if unix.FcntlFlock(uintptr(descriptor), unix.F_OFD_SETLK, &lock) != nil {
		unix.Close(descriptor)
		return nil, failure("native database is busy; retry after the current package transaction finishes")
	}
	return func() { unix.Close(descriptor) }, nil
}

func (r *Resolver) newStage(platform prerequisites.Platform) (*nativeStage, error) {
	interpreter := "/usr/bin/python3"
	if platform.OS == "rhel" {
		interpreter = "/usr/bin/python3.9"
	}
	resolved, err := filepath.EvalSymlinks(interpreter)
	if err != nil || providedFile(resolved, true) != nil {
		return nil, failure("native resolution requires the provided OS Python and DNF foundation")
	}
	root, err := os.MkdirTemp("/tmp", "bootwright-native-")
	if err != nil {
		return nil, failure("native resolver staging is unavailable")
	}
	stage := &nativeStage{root: root, work: filepath.Join(root, "work"), script: filepath.Join(root, "native_resolution.py"), interpreter: resolved, uid: os.Geteuid(), gid: os.Getegid()}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	asset := ansible.Automation()["collections/ansible_collections/bootwright/core/plugins/module_utils/native_resolution.py"]
	if len(asset) == 0 || os.Mkdir(stage.work, 0700) != nil || os.WriteFile(stage.script, asset, 0600) != nil {
		return nil, failure("native resolution helper is unavailable")
	}
	stage.snapshot, err = r.stageDatabase(platform)
	if err != nil {
		return nil, err
	}
	if os.Geteuid() == 0 {
		stage.uid, stage.gid = unprivilegedID, unprivilegedID
		if os.Chmod(root, 0755) != nil || os.Chmod(stage.script, 0444) != nil {
			return nil, failure("native resolver privilege separation failed")
		}
	}
	cleanup = false
	return stage, nil
}
func providedFile(path string, executable bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	observed, ok := info.Sys().(*syscall.Stat_t)
	if !ok || observed.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || executable && info.Mode().Perm()&0111 == 0 {
		return errors.New("provided file authority")
	}
	for directory := filepath.Dir(path); ; directory = filepath.Dir(directory) {
		info, err = os.Lstat(directory)
		if err != nil || !info.IsDir() {
			return errors.New("provided directory authority")
		}
		observed, ok = info.Sys().(*syscall.Stat_t)
		if !ok || observed.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return errors.New("provided directory authority")
		}
		if directory == "/" {
			break
		}
	}
	return nil
}

// copyDatabase returns the snapshot root and the identity the live database had
// while it was copied, so a later operation can tell whether that copy still
// describes the installed state.
func copyDatabase(platform prerequisites.Platform, work string) (string, databaseIdentity, error) {
	var identity databaseIdentity
	directory, err := databaseDirectory(platform)
	if err != nil {
		return "", identity, err
	}
	release, err := lockDatabase(directory)
	if err != nil {
		return "", identity, err
	}
	defer release()
	root := filepath.Join(work, "snapshot")
	target := filepath.Join(root, strings.TrimPrefix(directory, "/"))
	if os.MkdirAll(target, 0700) != nil {
		return "", identity, failure("native database snapshot staging failed")
	}
	var total int64
	for index, name := range databaseMembers {
		fd, err := unix.Open(directory+"/"+name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.ENOENT) && strings.HasSuffix(name, "-wal") {
			continue
		}
		if err != nil {
			return "", identity, failure("native SQLite database is unavailable")
		}
		input := os.NewFile(uintptr(fd), name)
		before, err := input.Stat()
		if err != nil {
			input.Close()
			return "", identity, failure("native database snapshot failed")
		}
		statinfo, ok := before.Sys().(*syscall.Stat_t)
		total += before.Size()
		if !ok || statinfo.Uid != 0 || !before.Mode().IsRegular() || before.Mode().Perm()&0022 != 0 || total > 512<<20 {
			input.Close()
			return "", identity, failure("native database snapshot exceeds its trusted bound")
		}
		output, err := os.OpenFile(filepath.Join(target, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			input.Close()
			return "", identity, failure("native database snapshot staging failed")
		}
		copied, copyErr := io.Copy(output, io.LimitReader(input, before.Size()+1))
		closeErr := output.Close()
		after, statErr := input.Stat()
		input.Close()
		if copyErr != nil || closeErr != nil || statErr != nil || copied != before.Size() || !sameFile(before, after) {
			return "", identity, failure("native database changed while creating its read-only snapshot")
		}
		identity.Files[index] = fileIdentity{Present: true, Device: uint64(statinfo.Dev), Inode: statinfo.Ino, Size: before.Size(), Modified: statinfo.Mtim.Nano(), Changed: statinfo.Ctim.Nano()}
	}
	return root, identity, nil
}
func sameFile(a, b os.FileInfo) bool {
	before, beforeOK := a.Sys().(*syscall.Stat_t)
	after, afterOK := b.Sys().(*syscall.Stat_t)
	return beforeOK && afterOK && before.Uid == after.Uid && before.Gid == after.Gid && before.Nlink == after.Nlink && before.Ctim == after.Ctim && a.Size() == b.Size() && a.ModTime() == b.ModTime() && a.Mode() == b.Mode() && os.SameFile(a, b)
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > b.limit {
		return 0, errors.New("native helper output limit")
	}
	return b.Buffer.Write(data)
}
func (s *nativeStage) run(ctx context.Context, request helperRequest) ([]byte, error) {
	if os.Geteuid() == 0 {
		err := filepath.WalkDir(s.work, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("native staging symlink")
			}
			return os.Chown(path, s.uid, s.gid)
		})
		if err != nil {
			return nil, failure("native resolver staging privilege separation failed")
		}
	}
	input, err := json.Marshal(request)
	if err != nil || len(input) > 2<<20 {
		return nil, failure("native resolver request is excessive")
	}
	command := exec.CommandContext(ctx, s.interpreter, "-I", "-B", s.script, s.work)
	command.Dir = s.work
	command.Env = []string{"PATH=/usr/sbin:/usr/bin", "LANG=C", "LC_ALL=C", "PYTHONDONTWRITEBYTECODE=1", "HOME=" + filepath.Join(s.work, "home")}
	command.Stdin = bytes.NewReader(input)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if os.Geteuid() == 0 {
		command.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(s.uid), Gid: uint32(s.gid), NoSetGroups: false}
	}
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	output := &boundedBuffer{limit: 8 << 20}
	diagnostic := &boundedBuffer{limit: 65536}
	command.Stdout = output
	command.Stderr = diagnostic
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failure("the provided native package solver refused its isolated dependency operation")
	}
	return slices.Clone(output.Bytes()), nil
}
func strictDecode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("native helper trailing JSON")
	}
	return nil
}
func failure(message string) error {
	return diagnostics.NewFailureWithRemediation("controller.setup", message, "", "Restore the provided OS package-manager foundation and approved repository access, then retry setup.")
}
