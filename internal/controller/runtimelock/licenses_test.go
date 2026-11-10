package runtimelock

import (
	"archive/zip"
	"io"
	"io/fs"
	"net/mail"
	"strings"
	"testing"
)

func TestEveryLockedDistributionNamesItsLicenseAndTheTableNamesNoOther(t *testing.T) {
	lock, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	table, err := Licenses()
	if err != nil {
		t.Fatal(err)
	}
	locked := map[string]bool{}
	files := []File{lock.Installer()}
	for _, abi := range lock.PythonABIs() {
		abiFiles, _ := lock.Files(abi)
		files = append(files, abiFiles...)
	}
	for _, file := range files {
		key := file.Name + "@" + file.Version
		locked[key] = true
		if license, found := LicenseOf(file); !found || license == "" {
			t.Errorf("%s names no license in licenses.json; run scripts/runtime-lock generate", key)
		}
	}
	for key := range table {
		if !locked[key] {
			t.Errorf("licenses.json names %s, which the lock does not", key)
		}
	}
}

func TestACarriedWheelDeclaresTheLicenseTheTableNames(t *testing.T) {
	table, err := Licenses()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	locked := map[string]File{lock.Installer().Filename: lock.Installer()}
	for _, abi := range lock.PythonABIs() {
		files, _ := lock.Files(abi)
		for _, file := range files {
			locked[file.Filename] = file
		}
	}
	carried := Carried()
	entries, err := fs.ReadDir(carried, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		file, found := locked[entry.Name()]
		if !found {
			continue
		}
		data, err := fs.ReadFile(carried, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		declared, classifiers := wheelLicenseDeclaration(t, data)
		recorded := table[file.Name+"@"+file.Version]
		switch {
		case declared != "":
			if declared != recorded.License || recorded.Source != "License-Expression" {
				t.Errorf("%s declares %s and licenses.json records %s from %s", entry.Name(), declared, recorded.License, recorded.Source)
			}
		case recorded.Source == "Classifier":
			if len(classifiers) != 1 || classifiers[0] != classifierLicenses[recorded.License] {
				t.Errorf("%s names the classifiers %v, which are not the one unambiguous classifier of %s", entry.Name(), classifiers, recorded.License)
			}
		case !strings.HasPrefix(recorded.Source, "reviewed:"):
			t.Errorf("%s declares no License-Expression, so licenses.json must record a classifier or a reviewed license", entry.Name())
		}
	}
}

var classifierLicenses = map[string]string{
	"MIT":        "License :: OSI Approved :: MIT License",
	"Apache-2.0": "License :: OSI Approved :: Apache Software License",
	"ISC":        "License :: OSI Approved :: ISC License (ISCL)",
	"MPL-2.0":    "License :: OSI Approved :: Mozilla Public License 2.0 (MPL 2.0)",
}

func wheelLicenseDeclaration(t *testing.T, wheel []byte) (string, []string) {
	t.Helper()
	archive, err := zip.NewReader(strings.NewReader(string(wheel)), int64(len(wheel)))
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range archive.File {
		if !strings.HasSuffix(member.Name, ".dist-info/METADATA") || strings.Count(member.Name, "/") != 1 {
			continue
		}
		reader, err := member.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		message, err := mail.ReadMessage(io.LimitReader(reader, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		var classifiers []string
		for _, classifier := range message.Header["Classifier"] {
			if strings.HasPrefix(classifier, "License ::") {
				classifiers = append(classifiers, classifier)
			}
		}
		return message.Header.Get("License-Expression"), classifiers
	}
	t.Fatal("a carried wheel holds no METADATA")
	return "", nil
}

func TestALicenseTableThatBreaksItsShapeIsRefused(t *testing.T) {
	entry := func(name, version, license, source string) string {
		return `{"name":"` + name + `","version":"` + version + `","license":"` + license + `","source":"` + source + `"}`
	}
	table := func(format string, entries ...string) string {
		return `{"format":"` + format + `","distributions":[` + strings.Join(entries, ",") + `]}`
	}
	good := []string{entry("jinja2", "3.1.6", "BSD-3-Clause", "License-Expression"), entry("pyyaml", "6.0.3", "MIT AND Apache-2.0", "Classifier")}
	if _, err := ParseLicenses([]byte(table(LicensesFormat, good...))); err != nil {
		t.Fatalf("a well-formed table was refused: %v", err)
	}
	for name, data := range map[string]string{
		"another format":    table("bootwright.controller.runtime-licenses-v0", good...),
		"a repeat":          table(LicensesFormat, good[0], good[0]),
		"no license":        table(LicensesFormat, entry("jinja2", "3.1.6", "", "x")),
		"parentheses":       table(LicensesFormat, entry("jinja2", "3.1.6", "(MIT OR ISC)", "x")),
		"a dangling AND":    table(LicensesFormat, entry("jinja2", "3.1.6", "MIT AND", "x")),
		"no source":         table(LicensesFormat, entry("jinja2", "3.1.6", "MIT", "")),
		"a bad name":        table(LicensesFormat, entry("Jinja2", "3.1.6", "MIT", "x")),
		"an unknown field":  strings.Replace(table(LicensesFormat, good...), `"distributions"`, `"extra":1,"distributions"`, 1),
		"two documents":     table(LicensesFormat, good...) + "{}",
		"not a JSON object": "[]",
	} {
		if _, err := ParseLicenses([]byte(data)); err == nil {
			t.Errorf("%s was admitted", name)
		}
	}
}
