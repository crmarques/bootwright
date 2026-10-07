//go:build linux && amd64

package nativelocal

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// foundationQueryFormat prints, for each installed instance, one identity line
// in rpmQueryFormat's fields followed by its file digest algorithm, then one
// line per file holding its path, digest and link target, each empty where rpm
// records none.
const foundationQueryFormat = "%{NAME}\t%{EPOCH}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\t%{RSAHEADER:pgpsig}\t%{SIGPGP:pgpsig}\t%{FILEDIGESTALGO}\n[%{FILENAMES}\t%{FILEDIGESTS}\t%{FILELINKTOS}\n]"

// foundationPackages are the only packages whose builds a foundation
// inspection reads, so nothing else ever reaches rpm's command line.
var foundationPackages = []string{"glibc", "libgcc"}

var _ prerequisites.FoundationBuildReader = (*Resolver)(nil)

const (
	foundationQueryLimit  = 1 << 20
	foundationLineLimit   = 16384
	foundationFileLimit   = 8192
	sha256DigestAlgorithm = 8
)

// FoundationBuilds reads, from a snapshot of the host's own package database,
// every installed instance of the named execution foundation packages, whether
// a signature of the platform's vendor key covers it, and the digest and link
// target rpm records for each of its files. It contacts no repository and
// changes nothing.
func (r *Resolver) FoundationBuilds(ctx context.Context, platform prerequisites.Platform, names []string) ([]prerequisites.InstalledBuild, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, failure("native dependency inspection is unavailable")
	}
	keyID, err := vendorKeyFor(platform)
	if err != nil {
		return nil, err
	}
	if !foundationNames(names) {
		return nil, failure("foundation inspection must name the glibc and libgcc packages")
	}
	if providedFile("/usr/bin/rpm", true) != nil {
		return nil, failure("native readiness requires the provided OS rpm")
	}
	snapshot, err := r.stageDatabase(platform)
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var builds []prerequisites.InstalledBuild
	for _, name := range names {
		result, err := queryRPMFormat(bounded, snapshot, name, foundationQueryFormat, foundationQueryLimit)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		instances, err := parseFoundationQuery(name, result, keyID)
		if err != nil {
			return nil, err
		}
		builds = append(builds, instances...)
	}
	return builds, nil
}

// vendorKeyFor is the key ID rpm prints for a signature of the vendor key the
// platform's profile qualifies: the low 64 bits of that key's fingerprint.
func vendorKeyFor(platform prerequisites.Platform) (string, error) {
	repositories, err := profiles(platform, prerequisites.NativeRequirements{})
	if err != nil {
		return "", err
	}
	if len(repositories) == 0 || len(repositories[0].Signer) < 16 {
		return "", failure("native dependency resolution requires a supported Fedora 43 or RHEL 9.8 package-manager foundation")
	}
	signer := repositories[0].Signer
	return signer[len(signer)-16:], nil
}

func foundationNames(names []string) bool {
	if len(names) == 0 || len(names) > len(foundationPackages) {
		return false
	}
	for index, name := range names {
		if !slices.Contains(foundationPackages, name) || slices.Contains(names[:index], name) {
			return false
		}
	}
	return true
}

// parseFoundationQuery reads one package's foundation query. A package with no
// instance has none; any other refusal, and any line outside the format, is
// evidence rpm could not give, never an absence.
func parseFoundationQuery(name string, result operatorQuery, keyID string) ([]prerequisites.InstalledBuild, error) {
	unreadable := failure("the provided rpm reported unreadable foundation package evidence")
	if result.Exit != 0 {
		if string(result.Stdout) == "package "+name+" is not installed\n" && len(result.Stderr) == 0 {
			return []prerequisites.InstalledBuild{}, nil
		}
		return nil, failure("the provided rpm could not read the installed package database")
	}
	text := string(result.Stdout)
	if !strings.HasSuffix(text, "\n") {
		return nil, unreadable
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) > foundationLineLimit {
		return nil, unreadable
	}
	builds := []prerequisites.InstalledBuild{}
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if strings.HasPrefix(line, "/") {
			if len(builds) == 0 || len(fields) != 3 {
				return nil, unreadable
			}
			current := &builds[len(builds)-1]
			if !foundationFilePath(fields[0]) || len(current.Files) == foundationFileLimit || !foundationDigest(fields[1], current.DigestAlgorithm) || !foundationLinkTarget(fields[2]) {
				return nil, unreadable
			}
			if _, duplicate := current.Files[fields[0]]; duplicate {
				return nil, unreadable
			}
			current.Files[fields[0]] = prerequisites.InstalledBuildFile{SHA256: fields[1], LinkTo: fields[2]}
			continue
		}
		if len(fields) != 8 || fields[0] != name || !operatorText(fields[2]) || !operatorText(fields[3]) || !operatorText(fields[4]) || len(builds) == 16 {
			return nil, unreadable
		}
		epoch, valid := rpmNumber(fields[1], 2147483647)
		if !valid {
			return nil, unreadable
		}
		algorithm, valid := rpmNumber(fields[7], 255)
		if !valid {
			return nil, unreadable
		}
		signed, err := vendorSigned(fields[5:7], keyID)
		if err != nil {
			return nil, err
		}
		builds = append(builds, prerequisites.InstalledBuild{Name: name, Epoch: epoch, Version: fields[2], Release: fields[3], Architecture: fields[4], Signed: signed, DigestAlgorithm: algorithm, Files: map[string]prerequisites.InstalledBuildFile{}})
	}
	return builds, nil
}

// rpmNumber reads a numeric tag rpm prints in decimal, or as "(none)" when the
// header does not carry it, which reads as zero.
func rpmNumber(value string, maximum int) (int, bool) {
	if value == "(none)" {
		return 0, true
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 || number > maximum || strconv.Itoa(number) != value {
		return 0, false
	}
	return number, true
}

func foundationFilePath(name string) bool {
	return strings.HasPrefix(name, "/") && len(name) <= 4096 && !strings.ContainsAny(name, "\x00\r\n\t")
}

func foundationDigest(value string, algorithm int) bool {
	if value == "" {
		return true
	}
	if algorithm == sha256DigestAlgorithm && len(value) != 64 {
		return false
	}
	return len(value) <= 128 && strings.Trim(value, "0123456789abcdef") == ""
}

func foundationLinkTarget(value string) bool {
	return len(value) <= 4096 && !strings.ContainsAny(value, "\x00\r\n\t")
}
