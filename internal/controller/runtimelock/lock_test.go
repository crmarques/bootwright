package runtimelock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/canonicaljson"
)

// embeddedLock is a copy of the lock the build ships.
func embeddedLock(t *testing.T) []byte {
	t.Helper()
	return slices.Clone(embedded)
}

// parsedDocument returns the embedded lock's members for a test to edit.
func parsedDocument(t *testing.T) document {
	t.Helper()
	var doc document
	if err := canonicaljson.DecodeClosed(embeddedLock(t), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// parseEdited encodes the embedded lock after edit and parses the result.
func parseEdited(t *testing.T, edit func(*document)) error {
	t.Helper()
	doc := parsedDocument(t)
	edit(&doc)
	data, err := canonicaljson.Encode(doc, canonicaljson.Bare)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(data)
	return err
}

func fileNamed(files []File, name string) *File {
	for index := range files {
		if files[index].Name == name {
			return &files[index]
		}
	}
	return nil
}

// The shipped lock parses, and what it says is what setup needs: every ABI
// newest first with a wheel list holding the closure, the installer, a floor
// RHEL 9 meets and the releases the build was tested on.
func TestTheEmbeddedLockIsWellFormed(t *testing.T) {
	lock, err := Parse(embeddedLock(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lock.PythonABIs(), []string{"3.14", "3.13", "3.12"}; !slices.Equal(got, want) {
		t.Fatalf("ABIs = %v, want %v", got, want)
	}
	for _, abi := range lock.PythonABIs() {
		files, found := lock.Files(abi)
		if !found {
			t.Fatalf("no file list for %s", abi)
		}
		for _, name := range requiredPackages {
			if fileNamed(files, name) == nil {
				t.Errorf("%s lacks %s", abi, name)
			}
		}
		if core := fileNamed(files, "ansible-core"); core == nil || core.Version != lock.AnsibleCore() {
			t.Errorf("%s carries %+v, not ansible-core %s", abi, core, lock.AnsibleCore())
		}
	}
	if _, found := lock.Files("3.11"); found {
		t.Error("an ABI the lock does not name has files")
	}
	if installer := lock.Installer(); installer.Name != "pip" || installer.Filename != "pip-"+installer.Version+"-py3-none-any.whl" {
		t.Errorf("installer = %+v", installer)
	}
	if lock.GlibcFloor() == "" || len(lock.Tested()) == 0 || len(lock.Digest()) != 64 {
		t.Errorf("floor %q, tested %v, digest %q", lock.GlibcFloor(), lock.Tested(), lock.Digest())
	}
}

// A file may only be fetched from the one host the lock may name, over https,
// at a package path ending in its own filename.
func TestEveryLockedURLIsOnFilesPythonhosted(t *testing.T) {
	lock, err := Parse(embeddedLock(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range append(mustFiles(t, lock, "3.12"), lock.Installer()) {
		if !strings.HasPrefix(file.URL, "https://files.pythonhosted.org/packages/") {
			t.Errorf("%s is fetched from %s", file.Filename, file.URL)
		}
	}
	good := mustFiles(t, lock, "3.12")[0].URL
	for name, url := range map[string]string{
		"plain http":      strings.Replace(good, "https:", "http:", 1),
		"another host":    strings.Replace(good, "files.pythonhosted.org", "example.com", 1),
		"a lookalike":     strings.Replace(good, "files.pythonhosted.org", "files.pythonhosted.org.example.com", 1),
		"a port":          strings.Replace(good, "files.pythonhosted.org", "files.pythonhosted.org:8443", 1),
		"credentials":     strings.Replace(good, "https://", "https://user@", 1),
		"a query":         good + "?token=1",
		"a fragment":      good + "#x",
		"another path":    strings.Replace(good, "/packages/", "/simple/", 1),
		"another file":    strings.TrimSuffix(good, ".whl") + "x.whl",
		"an escaped path": strings.Replace(good, "/packages/", "/packages/%2e%2e/", 1),
		"a file URL":      "file:///tmp/" + mustFiles(t, lock, "3.12")[0].Filename,
	} {
		err := parseEdited(t, func(doc *document) { doc.ABIs["3.12"][0].URL = url })
		if err == nil || !strings.Contains(err.Error(), "files.pythonhosted.org/packages/") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func mustFiles(t *testing.T, lock Lock, abi string) []File {
	t.Helper()
	files, found := lock.Files(abi)
	if !found {
		t.Fatalf("no files for %s", abi)
	}
	return files
}

// Each ABI lists wheels that run on its own interpreter: a compiled wheel is
// tagged for that interpreter or for the stable ABI from an older one, and the
// rest are pure, so pip's own tag check never has to pick.
func TestEveryABICarriesItsOwnBinaryWheels(t *testing.T) {
	lock, err := Parse(embeddedLock(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, abi := range lock.PythonABIs() {
		tag := "cp3" + strings.TrimPrefix(abi, "3.")
		perInterpreter := 0
		for _, file := range mustFiles(t, lock, abi) {
			if strings.Contains(file.Filename, "-"+tag+"-"+tag+"-") {
				perInterpreter++
			}
		}
		if perInterpreter == 0 {
			t.Errorf("%s carries no wheel built for %s", abi, tag)
		}
	}
	retag := func(name, from, to string) func(*document) {
		return func(doc *document) {
			file := fileNamed(doc.ABIs["3.12"], name)
			file.Filename = strings.Replace(file.Filename, from, to, 1)
			file.URL = strings.Replace(file.URL, from, to, 1)
		}
	}
	for name, test := range map[string]struct {
		edit func(*document)
		want string
	}{
		"another interpreter's wheel":   {retag("pyyaml", "cp312-cp312", "cp313-cp313"), "do not run on Python 3.12"},
		"a stable ABI from a newer one": {retag("cryptography", "cp311-abi3", "cp313-abi3"), "do not run on Python 3.12"},
		"a musl wheel":                  {retag("pyyaml", "manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64", "musllinux_1_2_x86_64"), "not a manylinux x86_64 tag"},
		"another architecture":          {retag("pyyaml", "x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64", "aarch64.manylinux_2_17_aarch64.manylinux_2_28_aarch64"), "not a manylinux x86_64 tag"},
		"a pure wheel with an ABI":      {retag("jinja2", "py3-none-any", "py3-cp312-any"), "are not py3-none-any"},
		"another distribution":          {retag("jinja2", "jinja2-", "jinja3-"), "does not name jinja2"},
		"another version":               {retag("jinja2", "-3.1.6-", "-3.1.5-"), "does not name jinja2"},
		"a build tag":                   {retag("jinja2", "-3.1.6-py3", "-3.1.6-1-py3"), "five parts"},
		"not a wheel":                   {retag("jinja2", ".whl", ".tar.gz"), "five parts"},
	} {
		if err := parseEdited(t, test.edit); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: err = %v, want %q", name, err, test.want)
		}
	}
}

// RHEL 9 carries glibc 2.34, so a lock whose files need a newer glibc would
// admit no supported host; the floor also has to be what the files need.
func TestTheGlibcFloorAdmitsRHEL9(t *testing.T) {
	lock, err := Parse(embeddedLock(t))
	if err != nil {
		t.Fatal(err)
	}
	if glibcMinor(lock.GlibcFloor()) > glibcMinor("2.34") {
		t.Fatalf("the floor %s is above RHEL 9's 2.34", lock.GlibcFloor())
	}
	newer := func(doc *document) {
		file := fileNamed(doc.ABIs["3.12"], "pyyaml")
		file.Filename = strings.Replace(file.Filename, "manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64", "manylinux_2_35_x86_64", 1)
		file.URL = strings.Replace(file.URL, "manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64", "manylinux_2_35_x86_64", 1)
		doc.GlibcFloor = "2.35"
	}
	for name, test := range map[string]struct {
		edit func(*document)
		want string
	}{
		"a floor above RHEL 9":          {newer, "would refuse RHEL 9"},
		"a floor below what files need": {func(doc *document) { doc.GlibcFloor = "2.5" }, "the highest manylinux tag the files need is 2.17"},
		"a floor above what files need": {func(doc *document) { doc.GlibcFloor = "2.34" }, "the highest manylinux tag the files need is 2.17"},
		"a floor that is no glibc":      {func(doc *document) { doc.GlibcFloor = "3.0" }, "is not a glibc version"},
		"an unnumbered floor":           {func(doc *document) { doc.GlibcFloor = "" }, "is not a glibc version"},
	} {
		if err := parseEdited(t, test.edit); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: err = %v, want %q", name, err, test.want)
		}
	}
}

// White space and line endings in the file never move the digest, while any
// member's value does, and the digest is the format domain over the lock's
// compact canonical bytes.
func TestTheLockDigestIgnoresFormatting(t *testing.T) {
	original := embeddedLock(t)
	lock, err := Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, original); err != nil {
		t.Fatal(err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, compact.Bytes(), "\t", "    "); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"compact":            compact.Bytes(),
		"indented":           indented.Bytes(),
		"crlf":               bytes.ReplaceAll(original, []byte("\n"), []byte("\r\n")),
		"no final line feed": bytes.TrimSpace(original),
	} {
		other, err := Parse(data)
		if err != nil || other.Digest() != lock.Digest() {
			t.Errorf("%s: digest %q, err %v, want %q", name, other.Digest(), err, lock.Digest())
		}
	}
	sum := sha256.Sum256(append([]byte("bootwright.controller.runtime-lock-v1\x00"), compact.Bytes()...))
	if lock.Digest() != hex.EncodeToString(sum[:]) {
		t.Errorf("digest = %s, want the domain-separated sha256 of the compact lock", lock.Digest())
	}
	for name, edit := range map[string]func(*document){
		"a tested release": func(doc *document) { doc.Tested[0].Versions = append(doc.Tested[0].Versions, "45") },
		"a hash":           func(doc *document) { doc.Installer.SHA256 = strings.Repeat("0", 64) },
	} {
		doc := parsedDocument(t)
		edit(&doc)
		data, err := canonicaljson.Encode(doc, canonicaljson.Bare)
		if err != nil {
			t.Fatal(err)
		}
		if other, err := Parse(data); err != nil || other.Digest() == lock.Digest() {
			t.Errorf("%s: digest %q, err %v", name, other.Digest(), err)
		}
	}
}

// Parse accepts only the lock's one spelling, so a lock cannot say two things
// to two readers, and every other rule the venv build leans on holds.
func TestParseRefusesWhatTheBuildCannotInstall(t *testing.T) {
	original := string(embeddedLock(t))
	for name, test := range map[string]struct {
		data string
		want string
	}{
		"trailing data":     {original + `{}`, "not one JSON value"},
		"an unknown member": {strings.Replace(original, `"format"`, `"extra": 1, "format"`, 1), "canonical form"},
		"a case variant":    {strings.Replace(original, `"ansibleCore"`, `"ansiblecore"`, 1), "canonical form"},
		"a missing member":  {strings.Replace(original, `"glibcFloor": "2.17",`, ``, 1), "canonical form"},
		"empty input":       {"", "not one JSON value"},
		"not an object":     {"[]", "canonical form"},
	} {
		if _, err := Parse([]byte(test.data)); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: err = %v, want %q", name, err, test.want)
		}
	}
	for name, test := range map[string]struct {
		edit func(*document)
		want string
	}{
		"another format":            {func(doc *document) { doc.Format = "x" }, "lock format"},
		"a prerelease ansible-core": {func(doc *document) { doc.AnsibleCore = "2.21.4rc1" }, "not a release"},
		"two ansible-core versions": {func(doc *document) {
			file := fileNamed(doc.ABIs["3.13"], "ansible-core")
			file.URL = strings.Replace(file.URL, file.Version, "2.21.0", 1)
			file.Version, file.Filename = "2.21.0", "ansible_core-2.21.0-py3-none-any.whl"
		}, "ansible-core is 2.21.0, not the lock's"},
		"cryptography missing": {func(doc *document) {
			doc.ABIs["3.12"] = slices.DeleteFunc(doc.ABIs["3.12"], func(file File) bool { return file.Name == "cryptography" })
		}, "cryptography is missing"},
		"urllib3 missing": {func(doc *document) {
			doc.ABIs["3.14"] = slices.DeleteFunc(doc.ABIs["3.14"], func(file File) bool { return file.Name == "urllib3" })
		}, "urllib3 is missing"},
		"unsorted files": {func(doc *document) { slices.Reverse(doc.ABIs["3.13"]) }, "sorted by name"},
		"a duplicate file": {func(doc *document) {
			doc.ABIs["3.13"] = slices.Insert(doc.ABIs["3.13"], 1, doc.ABIs["3.13"][0])
		}, "sorted by name"},
		"an uppercase sha256": {func(doc *document) { doc.ABIs["3.12"][0].SHA256 = strings.ToUpper(doc.ABIs["3.12"][0].SHA256) }, "64 lowercase hex"},
		"a short sha256":      {func(doc *document) { doc.Installer.SHA256 = "abcd" }, "64 lowercase hex"},
		"a zero size":         {func(doc *document) { doc.ABIs["3.12"][0].Size = 0 }, "outside 1 to"},
		"a non-pip installer": {func(doc *document) { doc.Installer.Name = "uv" }, `is not "pip"`},
		"a compiled installer": {func(doc *document) {
			doc.Installer.Filename = "pip-" + doc.Installer.Version + "-cp312-cp312-manylinux_2_28_x86_64.whl"
			doc.Installer.URL = strings.Replace(doc.Installer.URL, "py3-none-any", "cp312-cp312-manylinux_2_28_x86_64", 1)
		}, "the installer must be a pure wheel"},
		"ABIs out of order":          {func(doc *document) { slices.Reverse(doc.PythonABIs) }, "newest first"},
		"a repeated ABI":             {func(doc *document) { doc.PythonABIs = append(doc.PythonABIs, "3.12") }, "newest first"},
		"a python 2 ABI":             {func(doc *document) { doc.PythonABIs[0] = "2.7" }, "not a Python 3 ABI"},
		"no ABIs":                    {func(doc *document) { doc.PythonABIs = []string{} }, "names no Python ABI"},
		"an ABI without files":       {func(doc *document) { delete(doc.ABIs, "3.13") }, "has no file list"},
		"files for another ABI":      {func(doc *document) { doc.ABIs["3.11"] = doc.ABIs["3.12"] }, "does not name"},
		"null tested":                {func(doc *document) { doc.Tested = nil }, "tested is null"},
		"a repeated release":         {func(doc *document) { doc.Tested = append(doc.Tested, doc.Tested[0]) }, "tested release"},
		"a release without versions": {func(doc *document) { doc.Tested[0].Versions = []string{} }, "tested release"},
		"a bad tested version":       {func(doc *document) { doc.Tested[0].Versions[0] = "43 beta" }, "not a release number"},
	} {
		if err := parseEdited(t, test.edit); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: err = %v, want %q", name, err, test.want)
		}
	}
	if err := parseEdited(t, func(doc *document) { doc.Tested = []Release{} }); err != nil {
		t.Errorf("an empty tested list: %v", err)
	}
}

// What a caller reads out of a lock is a copy, so no caller edits the lock
// every later reader and the digest rely on.
func TestALockHandsOutCopies(t *testing.T) {
	lock, err := Parse(embeddedLock(t))
	if err != nil {
		t.Fatal(err)
	}
	before := lock.Digest()
	abis := lock.PythonABIs()
	abis[0] = "9.9"
	files := mustFiles(t, lock, "3.12")
	files[0].Name = "changed"
	tested := lock.Tested()
	tested[0].Versions[0] = "changed"
	if lock.PythonABIs()[0] == "9.9" || mustFiles(t, lock, "3.12")[0].Name == "changed" || lock.Tested()[0].Versions[0] == "changed" {
		t.Fatal("a caller edited the lock")
	}
	if again, err := Parse(embeddedLock(t)); err != nil || again.Digest() != before {
		t.Fatalf("digest %q, err %v", again.Digest(), err)
	}
}
