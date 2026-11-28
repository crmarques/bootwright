package inventory

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/machine/diskselection"
)

func TestLiveDiskCheckRequiresAUniqueMatchForEveryIdentifier(t *testing.T) {
	want := diskselection.DiskIdentity{WWN: "0x5000000000000001", SerialNumber: "TEST-A", SizeBytes: 8192}
	root := DiskObservation{Device: "sda", DiskIdentity: want, Eligible: true, Sequence: 1}
	other := DiskObservation{Device: "sdb", DiskIdentity: diskselection.DiskIdentity{WWN: "0x5000000000000002", SerialNumber: "TEST-B", SizeBytes: 4096}, Eligible: true, Sequence: 2}
	wrongBytes := root
	wrongBytes.SizeBytes = 4096
	duplicateWWN := other
	duplicateWWN.WWN = root.WWN
	duplicateSerial := other
	duplicateSerial.SerialNumber = root.SerialNumber
	unreadable := other
	unreadable.WWN = ""
	unwritable := root
	unwritable.Eligible = false

	for _, tc := range []struct {
		name, result string
		disks        []DiskObservation
		stable       bool
		wwn, serial  int
		exact        int
	}{
		{"complete", "matched", []DiskObservation{root, other}, true, 1, 1, 1},
		{"capacity differs", "mismatch", []DiskObservation{wrongBytes, other}, true, 1, 1, 0},
		{"same WWN at another capacity", "mismatch", []DiskObservation{root, duplicateWWN}, true, 2, 1, 1},
		{"same serial at another capacity", "mismatch", []DiskObservation{root, duplicateSerial}, true, 1, 2, 1},
		{"wire identity unavailable", "unknown", []DiskObservation{root, unreadable}, true, 1, 1, 1},
		{"disk is not writable", "mismatch", []DiskObservation{unwritable}, true, 1, 1, 0},
		{"inventory changed", "unknown", []DiskObservation{root, other}, false, 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeDiskCheck(want, tc.disks, tc.stable)
			if got.Status != tc.result || got.WWNMatches != tc.wwn || got.SerialMatches == nil || *got.SerialMatches != tc.serial || got.ExactMatches != tc.exact {
				t.Fatalf("report %+v", got)
			}
		})
	}

	want.SerialNumber = ""
	got := summarizeDiskCheck(want, []DiskObservation{root}, true)
	if got.Status != "matched" || got.SerialMatches != nil || got.Disks[0].Serial != "unused" {
		t.Fatalf("report with no requested serial %+v", got)
	}
}

func TestLiveDiskCheckComparesBootAndDeviceSequenceAcrossSnapshots(t *testing.T) {
	want := DiskCheckRequest{WWN: "0x5000000000000001", SizeBytes: 8192}
	for _, scenario := range []string{"same", "different boot", "substituted disk"} {
		t.Run(scenario, func(t *testing.T) {
			observations := []DiskObservation{{Device: "sda", DiskIdentity: want, Eligible: true, Sequence: 8}}
			calls := 0
			check := func(context.Context, bool) (DiskSnapshot, error) {
				calls++
				boot := "first"
				if calls == 2 {
					switch scenario {
					case "different boot":
						boot = "another"
					case "substituted disk":
						observations[0].Sequence++
					}
				}
				return DiskSnapshot{Boot: boot, Disks: observations, Complete: true}, nil
			}
			report, err := CheckDisk(context.Background(), want, check)
			if err != nil || calls != 2 || (report.Status == "matched") != (scenario == "same") {
				t.Fatalf("report %+v, err %v, calls %d", report, err, calls)
			}
		})
	}
}
