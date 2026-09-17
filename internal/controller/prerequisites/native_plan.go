package prerequisites

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func nativePlanFailure() error {
	return errors.New("native dependency plan is invalid or inconsistent")
}

// nativeRootNames are the exact packages each requirement installs as roots.
// It is the Go half of the solver's own table: a plan naming any other package
// as a root, or missing one of these, is refused before a transaction exists.
var nativeRootNames = map[string][]string{
	"podman":          {"podman"},
	"openssh":         {"openssh-clients"},
	"nmstate":         {"nmstate"},
	"libvirt":         {"libvirt-client"},
	"installer-media": {"lorax", "xorriso"},
	"hypervisor": {
		"libvirt-daemon", "libvirt-daemon-driver-network", "libvirt-daemon-driver-qemu",
		"libvirt-daemon-driver-storage-core", "qemu-img", "qemu-kvm", "swtpm", "swtpm-tools",
	},
}

// CanonicalNativePlan copies, orders, validates and hashes a resolved manifest.
func CanonicalNativePlan(value NativeResolvedPlan) (NativeResolvedPlan, error) {
	data, err := json.Marshal(value)
	var copied NativeResolvedPlan
	if err != nil || len(data) > 2<<20 || json.Unmarshal(data, &copied) != nil {
		return NativeResolvedPlan{}, nativePlanFailure()
	}
	value = copied
	// A requirement may name several roots, so the order within one key is part
	// of the canonical form rather than left to the solver.
	slices.SortFunc(value.Roots, func(a, b NativeRoot) int {
		if ordered := strings.Compare(a.Key, b.Key); ordered != 0 {
			return ordered
		}
		return strings.Compare(a.Package.Name, b.Package.Name)
	})
	slices.SortFunc(value.Repositories, func(a, b NativeRepository) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(value.Packages, func(a, b NativePackage) int { return strings.Compare(a.Source.ID, b.Source.ID) })
	slices.SortFunc(value.Actions, func(a, b NativeAction) int { return strings.Compare(a.SourceID, b.SourceID) })
	value.Digest = ""
	if err := validateNativeShape(value); err != nil {
		return NativeResolvedPlan{}, err
	}
	value.Digest, err = nativeDigest(value, true)
	return value, err
}

func ValidateNativePlan(value NativeResolvedPlan) error {
	canonical, err := CanonicalNativePlan(value)
	if err != nil {
		return err
	}
	got, _ := json.Marshal(value)
	want, _ := json.Marshal(canonical)
	if !slices.Equal(got, want) {
		return nativePlanFailure()
	}
	return nil
}

// NativeTransitionsDigest binds exactly the sorted planned package transitions.
func NativeTransitionsDigest(actions []NativeAction) (string, error) {
	if actions == nil || len(actions) > 512 {
		return "", nativePlanFailure()
	}
	return nativeDigest(actions, false)
}

func nativeDigest(value any, omitDigest bool) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var generic any
	if json.Unmarshal(data, &generic) != nil {
		return "", nativePlanFailure()
	}
	if omitDigest {
		delete(generic.(map[string]any), "digest")
	}
	data, err = json.Marshal(generic)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func validateNativeShape(value NativeResolvedPlan) error {
	if value.Format != "bootwright.native-plan-v1" || value.Platform.Architecture != "amd64" || value.Platform.OS != "fedora" && value.Platform.OS != "rhel" || !nativeText(value.Platform.Release, 32) || value.Solver != "dnf4" && value.Solver != "dnf5" || !nativeText(value.SolverVersion, 128) || !nativeSHA(value.BeforeSHA256) || !nativeSHA(value.AfterSHA256) || value.Roots == nil || value.Repositories == nil || value.Packages == nil || value.Actions == nil || len(value.Roots) > 16 || len(value.Repositories) > 8 || len(value.Packages) > 512 || len(value.Actions) > 512 {
		return nativePlanFailure()
	}
	if value.Platform.OS == "fedora" && value.Solver != "dnf5" || value.Platform.OS == "rhel" && value.Solver != "dnf4" {
		return nativePlanFailure()
	}
	for _, version := range []string{value.Requests.Python, value.Requests.Ansible, value.Requests.Helm, value.Requests.Govc, value.Requests.Virtctl} {
		if !api.ValidLexical("cli-version", version) {
			return nativePlanFailure()
		}
	}
	for _, version := range []string{value.Requests.Podman, value.Requests.OpenSSH, value.Requests.NMState, value.Requests.Libvirt} {
		if !api.ValidLexical("package-version", version) {
			return nativePlanFailure()
		}
	}
	// A plan resolved before this intent existed carries none, so it is read
	// only when the requirement that installs against it was selected.
	if value.Requirements.InstallerMedia && !api.ValidLexical("package-version", value.Requests.InstallerMedia) {
		return nativePlanFailure()
	}
	requests := map[string]string{"openssh": value.Requests.OpenSSH, "nmstate": value.Requests.NMState}
	if value.Requirements.ContainerRuntime {
		requests["podman"] = value.Requests.Podman
	}
	if value.Requirements.LibvirtClient {
		requests["libvirt"] = value.Requests.Libvirt
	}
	// The hypervisor runs the release its client speaks, so it shares that
	// intent rather than carrying one that could drift from it.
	if value.Requirements.Hypervisor {
		requests["hypervisor"] = value.Requests.Libvirt
	}
	if value.Requirements.InstallerMedia {
		requests["installer-media"] = value.Requests.InstallerMedia
	}
	// A requirement may install more than one root package, so the plan is
	// proved against exactly the packages its selected requirements name.
	expected := 0
	for key := range requests {
		expected += len(nativeRootNames[key])
	}
	if len(value.Roots) != expected {
		return nativePlanFailure()
	}
	repositories := map[string]bool{}
	for _, repo := range value.Repositories {
		if !nativeText(repo.ID, 128) || repositories[repo.ID] || !nativeHTTPS(repo.BaseURL) || !nativeSHA(repo.MetadataSHA256) {
			return nativePlanFailure()
		}
		repositories[repo.ID] = true
	}
	packages := map[string]NativePackage{}
	identities := map[NativeIdentity]string{}
	var totalBytes int64
	for _, pkg := range value.Packages {
		totalBytes += pkg.Source.Bytes
		if totalBytes > 4<<30 {
			return nativePlanFailure()
		}
		identity := NativeIdentity{pkg.Name, pkg.Epoch, pkg.Version, pkg.Release, pkg.Architecture}
		if !validNativeIdentity(identity) || !nativeText(pkg.Source.ID, 160) || packages[pkg.Source.ID].Name != "" || identities[identity] != "" || !nativeHTTPS(pkg.Source.URL) || !nativeSHA(pkg.Source.SHA256) || pkg.Source.Bytes <= 0 || pkg.Source.Bytes > 256<<20 || len(pkg.Signer) != 40 || strings.Trim(pkg.Signer, "0123456789abcdef") != "" {
			return nativePlanFailure()
		}
		approved := false
		for _, repo := range value.Repositories {
			approved = approved || strings.HasPrefix(pkg.Source.URL, strings.TrimSuffix(repo.BaseURL, "/")+"/")
		}
		if !approved {
			return nativePlanFailure()
		}
		packages[pkg.Source.ID] = pkg
		identities[identity] = pkg.Source.ID
	}
	used := map[string]bool{}
	rootNames := map[string]string{}
	rootIdentities := map[string]NativeIdentity{}
	for _, root := range value.Roots {
		requested, ok := requests[root.Key]
		if !ok || root.Requested != requested || !api.ValidLexical("package-version", requested) || !slices.Contains(nativeRootNames[root.Key], root.Package.Name) || !nativeRequestedVersion(root.Package, requested) || !validNativeIdentity(root.Package) || identities[root.Package] == "" || rootNames[root.Package.Name] != "" {
			return nativePlanFailure()
		}
		rootNames[root.Package.Name] = root.Requested
		rootIdentities[root.Package.Name] = root.Package
		used[identities[root.Package]] = true
	}
	affected := map[string]bool{}
	for _, action := range value.Actions {
		if action.Kind != "install" && action.Kind != "upgrade" && action.Kind != "downgrade" || !validNativeIdentity(action.After) || identities[action.After] != action.SourceID || affected[action.After.Name] || action.Reason != "root" && action.Reason != "dependency" {
			return nativePlanFailure()
		}
		if action.Kind == "install" {
			if action.Before != nil {
				return nativePlanFailure()
			}
		} else if action.Before == nil || !validNativeIdentity(*action.Before) || action.Before.Name != action.After.Name || action.Before.Architecture != action.After.Architecture || *action.Before == action.After {
			return nativePlanFailure()
		}
		if action.Kind == "downgrade" && (rootNames[action.After.Name] == "" || rootNames[action.After.Name] == "latest") {
			return nativePlanFailure()
		}
		if (action.Reason == "root") != (rootNames[action.After.Name] != "") {
			return nativePlanFailure()
		}
		if selected, ok := rootIdentities[action.After.Name]; ok && selected != action.After {
			return nativePlanFailure()
		}
		affected[action.After.Name] = true
		used[action.SourceID] = true
	}
	if len(used) != len(packages) || (len(value.Actions) == 0) != (value.BeforeSHA256 == value.AfterSHA256) {
		return nativePlanFailure()
	}
	return nil
}

func validNativeIdentity(value NativeIdentity) bool {
	return nativeText(value.Name, 128) && value.Epoch >= 0 && value.Epoch <= 2147483647 && nativeText(value.Version, 128) && nativeText(value.Release, 128) && (value.Architecture == "x86_64" || value.Architecture == "noarch")
}
func nativeText(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._+:-~^", c)) {
			return false
		}
	}
	return true
}
func nativeSHA(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
func nativeHTTPS(raw string) bool {
	value, err := url.Parse(raw)
	return err == nil && value.Scheme == "https" && value.Hostname() != "" && value.User == nil && value.RawQuery == "" && value.Fragment == "" && (value.Port() == "" || value.Port() == "443") && len(raw) <= 4096 && !strings.ContainsAny(raw, "\\\r\n\x00&<>") && nativeASCII(raw)
}

func nativeRequestedVersion(value NativeIdentity, requested string) bool {
	if requested == "latest" {
		return true
	}
	version := requested
	if prefix, rest, found := strings.Cut(version, ":"); found {
		if prefix != fmt.Sprint(value.Epoch) {
			return false
		}
		version = rest
	}
	if version, release, found := strings.Cut(version, "-"); found {
		return value.Version == version && value.Release == release
	}
	return value.Version == version
}
func nativeASCII(value string) bool {
	for _, character := range value {
		if character < 32 || character > 126 {
			return false
		}
	}
	return true
}
