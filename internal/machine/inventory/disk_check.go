package inventory

import (
	"context"
	"errors"
	"slices"

	"github.com/crmarques/bootwright/internal/machine/diskselection"
)

type DiskCheckRequest = diskselection.DiskIdentity

var ErrDiskSelector = errors.New("a nonzero NAA WWN and exact aligned byte capacity are required")

type DiskCheckReport struct {
	Status        string
	ExpectedBytes int64
	WWNMatches    int
	SerialMatches *int
	ExactMatches  int
	Disks         []DiskCheckRow
}

type DiskCheckRow struct {
	Device   string
	Bytes    int64
	WWN      string
	Serial   string
	Eligible bool
	Stable   bool
}

type DiskObservation struct {
	Device string
	diskselection.DiskIdentity
	Eligible bool
	Sequence uint64
}

type DiskSnapshot struct {
	Boot     string
	Disks    []DiskObservation
	Complete bool
}

func CheckDisk(ctx context.Context, want DiskCheckRequest, scan func(context.Context, bool) (DiskSnapshot, error)) (*DiskCheckReport, error) {
	if err := ValidateDiskCheck(want); err != nil {
		return nil, err
	}
	first, err := scan(ctx, want.SerialNumber != "")
	if err != nil {
		return nil, err
	}
	first.Disks = slices.Clone(first.Disks)
	again, err := scan(ctx, want.SerialNumber != "")
	if err != nil {
		return nil, err
	}
	stable := first.Complete && again.Complete && first.Boot != "" && first.Boot == again.Boot && slices.Equal(first.Disks, again.Disks)
	report := summarizeDiskCheck(want, again.Disks, stable)
	return &report, ctx.Err()
}

func ValidateDiskCheck(want DiskCheckRequest) error {
	if want.WWN == "" || want.Validate() != nil {
		return ErrDiskSelector
	}
	return nil
}

func summarizeDiskCheck(want DiskCheckRequest, observed []DiskObservation, stable bool) DiskCheckReport {
	report := DiskCheckReport{ExpectedBytes: want.SizeBytes, Status: "unknown", Disks: []DiskCheckRow{}}
	if want.SerialNumber != "" {
		report.SerialMatches = new(int)
	}
	complete := stable && len(observed) != 0
	for _, value := range observed {
		row := DiskCheckRow{
			Device: value.Device, Bytes: value.SizeBytes, Eligible: value.Eligible, Stable: stable && value.Sequence != 0,
			WWN: comparison(want.WWN, value.WWN), Serial: comparison(want.SerialNumber, value.SerialNumber),
		}
		if row.WWN == "match" {
			report.WWNMatches++
		}
		if row.Serial == "match" {
			*report.SerialMatches += 1
		}
		if row.WWN == "match" && (row.Serial == "match" || row.Serial == "unused") && row.Eligible && row.Stable && row.Bytes == want.SizeBytes {
			report.ExactMatches++
		}
		complete = complete && row.Stable && row.Bytes > 0 && row.WWN != "unreadable" && row.Serial != "unreadable"
		report.Disks = append(report.Disks, row)
	}
	if complete {
		report.Status = "mismatch"
		if report.WWNMatches == 1 && report.ExactMatches == 1 && (report.SerialMatches == nil || *report.SerialMatches == 1) {
			report.Status = "matched"
		}
	}
	return report
}

func comparison(want, observed string) string {
	switch {
	case want == "":
		return "unused"
	case observed == "":
		return "unreadable"
	case observed == want:
		return "match"
	default:
		return "different"
	}
}
