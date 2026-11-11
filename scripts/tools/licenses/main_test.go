package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func references(t *testing.T) map[string]canonicalText {
	t.Helper()
	loaded, err := loadReferences()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func reference(t *testing.T, id string) string {
	t.Helper()
	data, err := referenceFiles.ReadFile("references/" + id + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEachKnownTextIsClassifiedAsItselfBehindAnyCopyrightHeader(t *testing.T) {
	known := references(t)
	titles := map[string]string{"MIT": "The MIT License (MIT)", "ISC": "ISC License", "BSD-2-Clause": "BSD 2-Clause License", "BSD-3-Clause": "BSD 3-Clause License"}
	for _, id := range referenceIDs {
		for _, header := range []string{"", "Copyright (c) 2020 Someone\n\n", "Copyright (c) 2012 Alex Ogier. All rights reserved.\nCopyright 2019 A\n\n", titles[id] + "\n\nCopyright 2019 A\nCopyright 2020 B\n\n"} {
			body := reference(t, id)
			if first, rest, found := strings.Cut(body, "\n\n"); found && (id == "MIT" || id == "ISC") && strings.Contains(first, "License") {
				body = rest
			}
			if got, ok := classify(header+body, known); !ok || got != id {
				t.Errorf("%s behind %q classified as %q (%t)", id, header, got, ok)
			}
		}
	}
}

func TestTheGoAuthorsBSDTextWithBulletsAndAnOrganizationIsBSD3(t *testing.T) {
	text := "Copyright 2009 The Go Authors.\n\nRedistribution and use in source and binary forms, with or without\nmodification, are permitted provided that the following conditions are\nmet:\n\n   * Redistributions of source code must retain the above copyright\nnotice, this list of conditions and the following disclaimer.\n   * Redistributions in binary form must reproduce the above\ncopyright notice, this list of conditions and the following disclaimer\nin the documentation and/or other materials provided with the\ndistribution.\n   * Neither the name of Google LLC nor the names of its\ncontributors may be used to endorse or promote products derived from\nthis software without specific prior written permission.\n\n" +
		reference(t, "BSD-3-Clause")[strings.Index(reference(t, "BSD-3-Clause"), "THIS SOFTWARE IS PROVIDED"):]
	text = strings.Replace(text, "THE COPYRIGHT HOLDER OR CONTRIBUTORS", "THE COPYRIGHT OWNER OR CONTRIBUTORS", 1)
	if got, ok := classify(text, references(t)); !ok || got != "BSD-3-Clause" {
		t.Fatalf("classified as %q (%t)", got, ok)
	}
}

func TestATextThatDiffersFromEveryKnownTextIsNotGuessed(t *testing.T) {
	known := references(t)
	mit := reference(t, "MIT")
	for name, text := range map[string]string{
		"empty":                             "",
		"a header only":                     "Copyright (c) 2020 Someone\n",
		"an added clause":                   mit + "\nYou may not use the Software in a military program.\n",
		"a removed disclaimer":              strings.SplitN(mit, "THE SOFTWARE IS PROVIDED", 2)[0],
		"two licenses":                      "This project is covered by two licenses: MIT and Apache.\n\n" + mit + "\n" + reference(t, "Apache-2.0"),
		"another license":                   "GNU GENERAL PUBLIC LICENSE\nVersion 3, 29 June 2007\n",
		"a swapped word":                    strings.Replace(mit, "without restriction", "with restriction", 1),
		"a restriction after the copyright": "Copyright 2024 Foo. Commercial use requires a separate agreement.\n\n" + mitBody(mit),
		"a restricting license line":        "License: non-commercial use only\n\n" + mitBody(mit),
		"a restricting line between":        "MIT License\n\nCopyright 2024 Foo\nNon-commercial use only\n\n" + mitBody(mit),
		"a copyright line with a sentence":  "Copyright 2024 Foo and you may not use it for profit\n\n" + mitBody(mit),
		"a restricted third clause":         strings.Replace(reference(t, "BSD-3-Clause"), "the name of the copyright holder nor", "the name of Foo, except for commercial use, nor", 1),
		"a negated third clause":            strings.Replace(reference(t, "BSD-3-Clause"), "the name of the copyright holder nor", "the name of Foo not nor", 1),
	} {
		if got, ok := classify(text, known); ok {
			t.Errorf("%s was classified as %s", name, got)
		}
	}
}

func mitBody(mit string) string { return mit[strings.Index(mit, "Permission"):] }

func TestApacheTextWithOrWithoutItsAppendixIsApache(t *testing.T) {
	apache := reference(t, "Apache-2.0")
	cut, _, _ := strings.Cut(apache, "END OF TERMS AND CONDITIONS")
	filled := strings.Replace(apache, "Copyright [yyyy] [name of copyright owner]", "Copyright 2014 Someone Else", 1)
	if filled == apache {
		t.Fatal("the reference appendix holds no copyright template to fill")
	}
	for name, text := range map[string]string{"full": apache, "without the appendix": cut, "indented": "    " + strings.ReplaceAll(cut, "\n", "\n    "), "a filled appendix": filled, "the end marker alone": cut + "END OF TERMS AND CONDITIONS\n"} {
		if got, ok := classify(text, references(t)); !ok || got != "Apache-2.0" {
			t.Errorf("%s classified as %q (%t)", name, got, ok)
		}
	}
	for name, text := range map[string]string{
		"a GPL rider":                   apache + "\nAdditionally, the combined work is licensed under the GNU General Public License version 3.\n",
		"a Commons Clause rider":        cut + "END OF TERMS AND CONDITIONS\n\nCommons Clause: the Software may not be sold.\n",
		"a rider after a template line": strings.Replace(apache, "Copyright [yyyy] [name of copyright owner]", "Copyright 2014 Someone\nYou may not sell this.", 1),
	} {
		if got, ok := classify(text, references(t)); ok {
			t.Errorf("%s was classified as %s", name, got)
		}
	}
}

func moduleDirectory(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAModuleEntryTakesItsLicenseAndItsNoticeOrIsRefused(t *testing.T) {
	known := references(t)
	mit := "Copyright (c) 2023 Someone\n\n" + reference(t, "MIT")[strings.Index(reference(t, "MIT"), "Permission"):]
	entry, err := entryOf("example.test/a", "v1.0.0", moduleDirectory(t, map[string]string{"LICENSE": mit, "NOTICE": "attribution\n", "main.go": "package a\n"}), known)
	if err != nil || entry.License != "MIT" || entry.File != "LICENSE" || entry.Notice != "attribution\n" || entry.Text != mit {
		t.Fatalf("entry %+v: %v", entry, err)
	}
	odd := "A license nobody wrote down.\n"
	sum := sha256.Sum256([]byte(odd))
	for name, test := range map[string]struct {
		files map[string]string
		want  string
	}{
		"no license file":   {map[string]string{"main.go": "package a\n"}, "holds no license file"},
		"two license files": {map[string]string{"LICENSE": mit, "COPYING": mit}, "names two license files"},
		"an unknown text":   {map[string]string{"LICENSE": odd}, hex.EncodeToString(sum[:])},
	} {
		_, err := entryOf("example.test/a", "v1.0.0", moduleDirectory(t, test.files), known)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v, want a refusal naming %q", name, err, test.want)
		}
	}
	reviewedTexts = append(reviewedTexts, reviewedText{hex.EncodeToString(sum[:]), "Zlib", "test"})
	t.Cleanup(func() { reviewedTexts = reviewedTexts[:len(reviewedTexts)-1] })
	if entry, err := entryOf("example.test/a", "v1.0.0", moduleDirectory(t, map[string]string{"LICENSE": odd}), known); err != nil || entry.License != "Zlib" {
		t.Errorf("a reviewed text was not taken: %+v %v", entry, err)
	}
}

func TestTheCheckedInGoTableHoldsTextsEachClassifiedAsItsRecordedLicense(t *testing.T) {
	table, err := readTable(filepath.Join("..", "..", "..", "cmd", "bootwright", "go-licenses.json"))
	if err != nil {
		t.Fatal(err)
	}
	known := references(t)
	if len(table.Modules) < 2 || table.Modules[0].Path != stdlibPath {
		t.Fatalf("the table names %d entries and does not lead with the standard library", len(table.Modules))
	}
	for _, entry := range table.Modules {
		got, ok := classify(entry.Text, known)
		if !ok {
			sum := sha256.Sum256([]byte(entry.Text))
			ok = false
			for _, reviewed := range reviewedTexts {
				if reviewed.sha256 == hex.EncodeToString(sum[:]) && reviewed.license == entry.License {
					got, ok = entry.License, true
				}
			}
		}
		if !ok || got != entry.License || entry.File == "" {
			t.Errorf("%s %s records %s from %s, which classifies as %q (%t)", entry.Path, entry.Version, entry.License, entry.File, got, ok)
		}
	}
}

func wheelWith(t *testing.T, dir, name string, files map[string]string) {
	t.Helper()
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(out)
	for member, text := range files {
		w, err := archive.Create(member)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(text))
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

const sbomHead = `{"metadata":{"component":{"name":"bootwright","version":"v1.0.0"}},"components":[`

func TestTheNoticesReproduceEveryEmbeddedComponentsLicenseAndRefuseWhatTheyCannot(t *testing.T) {
	wheels := t.TempDir()
	wheelWith(t, wheels, "jinja2-3.1.6-py3-none-any.whl", map[string]string{"jinja2-3.1.6.dist-info/licenses/LICENSE.txt": "Jinja license text\n", "jinja2/__init__.py": ""})
	wheelWith(t, wheels, "markupsafe-3.0.4-cp312-x.whl", map[string]string{"markupsafe-3.0.4.dist-info/licenses/LICENSE.txt": "MarkupSafe text\n"})
	wheelWith(t, wheels, "markupsafe-3.0.4-cp314-x.whl", map[string]string{"markupsafe-3.0.4.dist-info/licenses/LICENSE.txt": "MarkupSafe text\n"})
	wheelWith(t, wheels, "old-1.0-py3-none-any.whl", map[string]string{"old-1.0.dist-info/COPYING": "Old text\n"})
	wheelWith(t, wheels, "bare-1.0-py3-none-any.whl", map[string]string{"bare/__init__.py": ""})
	table := moduleTable{Format: goTableFormat, Modules: []moduleEntry{
		{Path: "stdlib", License: "BSD-3-Clause", File: "LICENSE", Text: "Go text\n"},
		{Path: "example.test/m", Version: "v1.2.3", License: "MIT AND Apache-2.0", File: "LICENSE", Text: "M text\n", Notice: "M notice\n"},
	}}
	component := func(ref, name, version, scope, license string) string {
		licenses := ""
		switch {
		case strings.Contains(license, " "):
			licenses = `,"licenses":[{"expression":"` + license + `"}]`
		case license != "":
			licenses = `,"licenses":[{"license":{"id":"` + license + `"}}]`
		}
		return `{"bom-ref":"` + ref + `","name":"` + name + `","version":"` + version + `","scope":"` + scope + `"` + licenses + `}`
	}
	good := []string{
		component("go:stdlib@1.26.10", "stdlib", "1.26.10", "required", "BSD-3-Clause"),
		component("go:example.test/m@v1.2.3", "example.test/m", "v1.2.3", "required", "MIT AND Apache-2.0"),
		component("pypi:jinja2-3.1.6-py3-none-any.whl", "jinja2", "3.1.6", "required", "BSD-3-Clause"),
		component("pypi:markupsafe-3.0.4-cp312-x.whl", "markupsafe", "3.0.4", "required", "BSD-3-Clause"),
		component("pypi:markupsafe-3.0.4-cp314-x.whl", "markupsafe", "3.0.4", "required", "BSD-3-Clause"),
		component("pypi:old-1.0-py3-none-any.whl", "old", "1.0", "required", "MIT"),
		component("ansible:bootwright.core", "bootwright.core", "", "required", ""),
		component("tool:helm@4.3.1", "helm", "4.3.1", "optional", ""),
		component("pypi:absent-1.0-py3-none-any.whl", "absent", "1.0", "optional", ""),
	}
	compose := func(components ...string) []byte { return []byte(sbomHead + strings.Join(components, ",") + "]}") }
	notices, err := buildNotices(compose(good...), table, wheels)
	if err != nil {
		t.Fatal(err)
	}
	text := string(notices)
	for _, want := range []string{"Go text", "M text", "M notice", "Jinja license text", "MarkupSafe text", "Old text", "License: MIT AND Apache-2.0", "Wheels: markupsafe-3.0.4-cp312-x.whl, markupsafe-3.0.4-cp314-x.whl", "bootwright v1.0.0"} {
		if !strings.Contains(text, want) {
			t.Errorf("the notices omit %q", want)
		}
	}
	if strings.Count(text, "MarkupSafe text") != 1 || strings.Contains(text, "helm") || strings.Contains(text, "bootwright.core") {
		t.Error("the notices repeat a distribution or name a component the executable does not embed")
	}
	sum := sha256.Sum256(compose(good...))
	if !strings.Contains(text, "\nBill of materials: sha256:"+hex.EncodeToString(sum[:])+"\n") {
		t.Error("the notices do not name the digest of the bill of materials they were composed from")
	}
	other, err := buildNotices(append(compose(good...), '\n'), table, wheels)
	if err != nil || strings.Contains(string(other), hex.EncodeToString(sum[:])) {
		t.Errorf("the notices of a different bill of materials name the same digest (%v)", err)
	}
	again, _ := buildNotices(compose(good...), table, wheels)
	if string(again) != text {
		t.Error("two compositions of one input differ")
	}
	for name, test := range map[string]struct {
		components []string
		want       string
	}{
		"a component without a license":     {[]string{component("go:example.test/m@v1.2.3", "example.test/m", "v1.2.3", "required", "")}, "names no license"},
		"a module without table text":       {[]string{component("go:example.test/n@v1.0.0", "example.test/n", "v1.0.0", "required", "MIT")}, "make licenses"},
		"a module licensed differently":     {[]string{component("go:example.test/m@v1.2.3", "example.test/m", "v1.2.3", "required", "MIT")}, "licensed MIT"},
		"a wheel that is not there":         {[]string{component("pypi:gone-1.0-py3-none-any.whl", "gone", "1.0", "required", "MIT")}, "gone-1.0"},
		"a wheel with no license file":      {[]string{component("pypi:bare-1.0-py3-none-any.whl", "bare", "1.0", "required", "MIT")}, "no license file"},
		"a wheel named by a path":           {[]string{component("pypi:../jinja2-3.1.6-py3-none-any.whl", "jinja2", "3.1.6", "required", "MIT")}, "with a path"},
		"a kind with no source":             {[]string{component("npm:x@1", "x", "1", "required", "MIT")}, "no license source"},
		"no third-party component embedded": {[]string{component("ansible:bootwright.core", "bootwright.core", "", "required", "")}, "no embedded third-party"},
	} {
		if _, err := buildNotices(compose(test.components...), table, wheels); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v, want a refusal naming %q", name, err, test.want)
		}
	}
	if _, err := buildNotices([]byte("{}"), table, wheels); err == nil {
		t.Error("a document that is no bill of materials produced notices")
	}
}

func TestTheNoticesFileIsWrittenOrTheCommandFails(t *testing.T) {
	directory := t.TempDir()
	if err := run("notices", ".", filepath.Join(directory, "missing.json"), "", directory, filepath.Join(directory, "out.txt")); err == nil {
		t.Error("notices ran without a bill of materials")
	}
	if _, err := os.Stat(filepath.Join(directory, "out.txt")); err == nil {
		t.Error("a failed run left a notices file")
	}
	if err := run("unknown", ".", "", "", "", ""); err == nil {
		t.Error("an unknown command ran")
	}
}
