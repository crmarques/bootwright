package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewedClientScanRecordsAreBoundToThePinnedRelease(t *testing.T) {
	table := "../../../internal/controller/tooltable/tooltable.json"
	directory := "../clientassets/scanrecords"
	db := "file:///missing-go-advisory-fixture"
	source, err := openRecords(directory, db, table)
	if err != nil {
		t.Fatal(err)
	}
	pins, err := readTable(table)
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range pins {
		r := source.records[pin.Kind]
		if r.Version != pin.Default.Version || r.ReleaseSHA256 != pin.Default.SHA256 {
			t.Fatalf("unbound scan record for %s", pin.Kind)
		}
	}
	for _, suffix := range []string{".json.gz", ".gomod"} {
		data, err := os.ReadFile(filepath.Join(directory, "virtctl"+suffix))
		if err != nil {
			t.Fatal(err)
		}
		data[0] ^= 1
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "virtctl"+suffix), data, 0o600); err != nil {
			t.Fatal(err)
		}
		changed := source
		changed.directory = dir
		entry := pins[len(pins)-1]
		read := changed.scan
		if suffix == ".gomod" {
			read = changed.buildModules
		}
		if _, err := read(entry); err == nil || !strings.Contains(err.Error(), "missing or changed") {
			t.Fatalf("changed %s entered a scan: %v", suffix, err)
		}
	}
	tableBytes, err := os.ReadFile(table)
	if err != nil {
		t.Fatal(err)
	}
	changedTable := filepath.Join(t.TempDir(), "table.json")
	if err := os.WriteFile(changedTable, []byte(strings.Replace(string(tableBytes), pins[0].Default.SHA256, strings.Repeat("0", 64), 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openRecords(directory, db, changedTable); err == nil || !strings.Contains(err.Error(), "tool-records-refresh") {
		t.Fatalf("a different release digest retained its reviewed record: %v", err)
	}
}
