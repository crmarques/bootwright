// Package runtimelock parses and validates the lock of the controller's
// Python runtime: the exact wheels, per Python ABI, that setup installs into
// the venv, chosen when the lock is generated and never at setup. It performs
// no I/O.
package runtimelock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
)

// Format names the lock's shape and is the domain its digest is taken under.
const Format = "bootwright.controller.runtime-lock-v1"

// MaxGlibcFloor is the newest glibc a lock may require: RHEL 9 ships 2.34.
const MaxGlibcFloor = "2.34"

const (
	pythonhostedHost = "files.pythonhosted.org"
	maxFileSize      = 1 << 30
)

// File is one wheel: its distribution, the filename pip installs it by, and
// where to fetch it and what it must hash to.
type File struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// Release is a distribution release the build was tested on.
type Release struct {
	ID       string   `json:"id"`
	Versions []string `json:"versions"`
}

type document struct {
	Format      string            `json:"format"`
	AnsibleCore string            `json:"ansibleCore"`
	PythonABIs  []string          `json:"pythonABIs"`
	GlibcFloor  string            `json:"glibcFloor"`
	Tested      []Release         `json:"tested"`
	Installer   File              `json:"installer"`
	ABIs        map[string][]File `json:"abis"`
}

// Lock is a parsed, validated runtime lock. The zero value is empty.
type Lock struct {
	document document
	digest   string
}

// requiredPackages is ansible-core's declared closure plus the controller's
// one downloader; the venv installs without dependency resolution.
var requiredPackages = []string{
	"ansible-core", "cffi", "cryptography", "jinja2", "markupsafe", "packaging",
	"pycparser", "pyyaml", "resolvelib", "urllib3",
}

var (
	abiPattern     = regexp.MustCompile(`^3\.[0-9]{1,3}$`)
	glibcPattern   = regexp.MustCompile(`^2\.[0-9]{1,3}$`)
	versionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)
	corePattern    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	namePattern    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	abi3Pattern    = regexp.MustCompile(`^cp3([0-9]{1,3})$`)
	legacyGlibc    = map[string]int{"manylinux1_x86_64": 5, "manylinux2010_x86_64": 12, "manylinux2014_x86_64": 17}
	manylinuxTag   = regexp.MustCompile(`^manylinux_2_([0-9]{1,3})_x86_64$`)
)

// Parse reads a lock. The bytes must be the lock's canonical encoding apart
// from white space: closed members in their declared order, one value, nothing
// after it. Every rule below is enforced here, so a lock that parses is one
// the venv build can install without further checks.
func Parse(data []byte) (Lock, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return Lock{}, errors.New("the runtime lock is not one JSON value")
	}
	var doc document
	if err := canonicaljson.Decode(compact.Bytes(), &doc, canonicaljson.Bare); err != nil {
		return Lock{}, fmt.Errorf("the runtime lock is not in its canonical form: %w", err)
	}
	if err := validate(doc); err != nil {
		return Lock{}, err
	}
	canonical, err := canonicaljson.Encode(doc, canonicaljson.Bare)
	if err != nil {
		return Lock{}, err
	}
	sum := sha256.Sum256(append([]byte(Format+"\x00"), canonical...))
	return Lock{document: doc, digest: hex.EncodeToString(sum[:])}, nil
}

// Digest is the lowercase SHA-256 of the lock's canonical re-encoding under
// the lock's format domain, so white space in the file never moves it.
func (l Lock) Digest() string { return l.digest }

// AnsibleCore is the one ansible-core version every ABI carries.
func (l Lock) AnsibleCore() string { return l.document.AnsibleCore }

// PythonABIs lists the locked interpreter ABIs, newest first.
func (l Lock) PythonABIs() []string { return slices.Clone(l.document.PythonABIs) }

// Files lists the wheels of one ABI, sorted by name, and reports whether the
// lock carries the ABI.
func (l Lock) Files(abi string) ([]File, bool) {
	files, found := l.document.ABIs[abi]
	return slices.Clone(files), found
}

// Installer is the pip wheel that installs the rest.
func (l Lock) Installer() File { return l.document.Installer }

// GlibcFloor is the oldest glibc the locked binary wheels run on.
func (l Lock) GlibcFloor() string { return l.document.GlibcFloor }

// Tested lists the releases the build was tested on, which only inform.
func (l Lock) Tested() []Release {
	tested := make([]Release, 0, len(l.document.Tested))
	for _, release := range l.document.Tested {
		release.Versions = slices.Clone(release.Versions)
		tested = append(tested, release)
	}
	return tested
}

func validate(doc document) error {
	if doc.Format != Format {
		return fmt.Errorf("the runtime lock format is %q, not %q", doc.Format, Format)
	}
	if !corePattern.MatchString(doc.AnsibleCore) {
		return fmt.Errorf("the ansible-core version %q is not a release", doc.AnsibleCore)
	}
	if err := validateABIs(doc); err != nil {
		return err
	}
	if err := validateTested(doc.Tested); err != nil {
		return err
	}
	if err := validateFile(doc.Installer, "pip", 0); err != nil {
		return fmt.Errorf("installer: %w", err)
	}
	floor := 0
	for _, abi := range doc.PythonABIs {
		needed, err := validateABI(doc, abi)
		if err != nil {
			return fmt.Errorf("abi %s: %w", abi, err)
		}
		floor = max(floor, needed)
	}
	if want := "2." + strconv.Itoa(floor); doc.GlibcFloor != want {
		return fmt.Errorf("the glibc floor is %s, but the highest manylinux tag the files need is %s", doc.GlibcFloor, want)
	}
	return nil
}

func validateABIs(doc document) error {
	if len(doc.PythonABIs) == 0 {
		return errors.New("the runtime lock names no Python ABI")
	}
	previous := 0
	for _, abi := range doc.PythonABIs {
		if !abiPattern.MatchString(abi) {
			return fmt.Errorf("%q is not a Python 3 ABI", abi)
		}
		minor := abiMinor(abi)
		if previous != 0 && minor >= previous {
			return fmt.Errorf("pythonABIs must list each ABI once, newest first; %s breaks that", abi)
		}
		previous = minor
		if _, found := doc.ABIs[abi]; !found {
			return fmt.Errorf("ABI %s has no file list", abi)
		}
	}
	if len(doc.ABIs) != len(doc.PythonABIs) {
		return errors.New("abis holds a file list for an ABI pythonABIs does not name")
	}
	if !glibcPattern.MatchString(doc.GlibcFloor) {
		return fmt.Errorf("the glibc floor %q is not a glibc version", doc.GlibcFloor)
	}
	if glibcMinor(doc.GlibcFloor) > glibcMinor(MaxGlibcFloor) {
		return fmt.Errorf("the glibc floor %s is above %s, which would refuse RHEL 9", doc.GlibcFloor, MaxGlibcFloor)
	}
	return nil
}

func validateTested(tested []Release) error {
	if tested == nil {
		return errors.New("tested is null; an empty list is spelled []")
	}
	seen := map[string]bool{}
	for _, release := range tested {
		if !idPattern.MatchString(release.ID) || seen[release.ID] || len(release.Versions) == 0 {
			return fmt.Errorf("tested release %q is not one distinct id with versions", release.ID)
		}
		seen[release.ID] = true
		for _, version := range release.Versions {
			if !versionPattern.MatchString(version) {
				return fmt.Errorf("tested %s version %q is not a release number", release.ID, version)
			}
		}
	}
	return nil
}

// validateABI checks one ABI's files and returns the glibc minor they need.
func validateABI(doc document, abi string) (int, error) {
	files := doc.ABIs[abi]
	names := make([]string, 0, len(files))
	needed := 0
	for _, file := range files {
		if err := validateFile(file, "", abiMinor(abi)); err != nil {
			return 0, fmt.Errorf("%s: %w", file.Name, err)
		}
		required, err := glibcNeeded(file)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", file.Name, err)
		}
		needed = max(needed, required)
		names = append(names, file.Name)
		if file.Name == "ansible-core" && file.Version != doc.AnsibleCore {
			return 0, fmt.Errorf("ansible-core is %s, not the lock's %s", file.Version, doc.AnsibleCore)
		}
	}
	if !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) {
		return 0, errors.New("the files must be sorted by name, one per distribution")
	}
	for _, required := range requiredPackages {
		if !slices.Contains(names, required) {
			return 0, fmt.Errorf("%s is missing", required)
		}
	}
	if needed == 0 {
		return 0, errors.New("no binary wheel names the glibc the files need")
	}
	return needed, nil
}

// validateFile checks one file's fields and its wheel tags against the ABI
// minor; an installer, with minor 0, must be a pure wheel.
func validateFile(file File, name string, minor int) error {
	switch {
	case !namePattern.MatchString(file.Name):
		return fmt.Errorf("the name %q is not a normalized distribution name", file.Name)
	case name != "" && file.Name != name:
		return fmt.Errorf("the name %q is not %q", file.Name, name)
	case !versionPattern.MatchString(file.Version):
		return fmt.Errorf("the version %q is not a release number", file.Version)
	case !digestPattern.MatchString(file.SHA256):
		return errors.New("the sha256 is not 64 lowercase hex digits")
	case file.Size < 1 || file.Size > maxFileSize:
		return fmt.Errorf("the size %d is outside 1 to %d", file.Size, maxFileSize)
	}
	if err := validateURL(file); err != nil {
		return err
	}
	tags, err := wheelTags(file)
	if err != nil {
		return err
	}
	if tags.platform == "any" {
		if tags.python != "py3" && tags.python != "py2.py3" || tags.abi != "none" {
			return fmt.Errorf("the pure wheel tags %s-%s-%s are not py3-none-any", tags.python, tags.abi, tags.platform)
		}
		return nil
	}
	if minor == 0 {
		return errors.New("the installer must be a pure wheel")
	}
	perInterpreter := tags.python == "cp3"+strconv.Itoa(minor) && tags.abi == tags.python
	stable := abi3Pattern.FindStringSubmatch(tags.python)
	if !perInterpreter && !(tags.abi == "abi3" && stable != nil && atoi(stable[1]) <= minor) {
		return fmt.Errorf("the wheel tags %s-%s do not run on Python 3.%d", tags.python, tags.abi, minor)
	}
	return nil
}

func validateURL(file File) error {
	address, err := url.Parse(file.URL)
	if err != nil || address.Scheme != "https" || address.Host != pythonhostedHost || address.User != nil ||
		address.RawQuery != "" || address.Fragment != "" || address.RawPath != "" ||
		!strings.HasPrefix(address.Path, "/packages/") || !strings.HasSuffix(address.Path, "/"+file.Filename) {
		return fmt.Errorf("the URL %q is not an https://%s/packages/ path ending in the filename", file.URL, pythonhostedHost)
	}
	return nil
}

type tagSet struct{ python, abi, platform string }

// wheelTags splits a wheel filename of exactly five parts and checks that it
// names the file's distribution and version.
func wheelTags(file File) (tagSet, error) {
	stem, found := strings.CutSuffix(file.Filename, ".whl")
	parts := strings.Split(stem, "-")
	if !found || len(parts) != 5 || strings.ContainsAny(file.Filename, "/\\") {
		return tagSet{}, fmt.Errorf("the filename %q is not a wheel name of five parts", file.Filename)
	}
	if normalizeDistribution(parts[0]) != normalizeDistribution(file.Name) || parts[1] != file.Version {
		return tagSet{}, fmt.Errorf("the filename %q does not name %s %s", file.Filename, file.Name, file.Version)
	}
	return tagSet{python: parts[2], abi: parts[3], platform: parts[4]}, nil
}

// glibcNeeded is the glibc minor a wheel needs: 0 for a pure wheel, else the
// oldest manylinux tag of its platform, as any one of them admits the host.
func glibcNeeded(file File) (int, error) {
	tags, err := wheelTags(file)
	if err != nil || tags.platform == "any" {
		return 0, err
	}
	needed := 0
	for _, tag := range strings.Split(tags.platform, ".") {
		minor, ok := legacyGlibc[tag]
		if match := manylinuxTag.FindStringSubmatch(tag); match != nil {
			minor, ok = atoi(match[1]), true
		}
		if !ok {
			return 0, fmt.Errorf("the platform tag %q is not a manylinux x86_64 tag", tag)
		}
		if needed == 0 || minor < needed {
			needed = minor
		}
	}
	return needed, nil
}

func normalizeDistribution(name string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(name))
}

func abiMinor(abi string) int { return atoi(strings.TrimPrefix(abi, "3.")) }

func glibcMinor(glibc string) int { return atoi(strings.TrimPrefix(glibc, "2.")) }

func atoi(digits string) int {
	number, _ := strconv.Atoi(digits)
	return number
}
