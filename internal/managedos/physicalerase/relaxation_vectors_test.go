package physicalerase

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const eraseRelaxationVectors = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/physical_erase_relaxation.json"

type eraseRelaxationVector struct {
	Relaxation       string        `json:"relaxation"`
	RelaxationBase64 *string       `json:"relaxationBase64"`
	Targets          []EraseTarget `json:"targets"`
	Coverage         []int64       `json:"coverage"`
}

func TestEraseRelaxationVectorsMatchTheWorkersRelaxedTargets(t *testing.T) {
	data, err := os.ReadFile(eraseRelaxationVectors)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Declared []EraseTarget           `json:"declared"`
		Vectors  []eraseRelaxationVector `json:"vectors"`
		Retain   string                  `json:"retain"`
		Refused  []string                `json:"refused"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) != 2 || len(fixture.Refused) == 0 {
		t.Fatalf("vectors = %d, refused = %d", len(fixture.Vectors), len(fixture.Refused))
	}
	if fixture.Retain != base64.StdEncoding.EncodeToString([]byte(lifecycle.EraseRetain)) {
		t.Fatalf("retain = %q", fixture.Retain)
	}
	for _, vector := range fixture.Vectors {
		t.Run(vector.Relaxation, func(t *testing.T) {
			if vector.Relaxation == "" {
				if vector.RelaxationBase64 != nil {
					t.Fatal("an unrelaxed run carries no relaxationBase64")
				}
			} else if vector.RelaxationBase64 == nil || *vector.RelaxationBase64 != base64.StdEncoding.EncodeToString([]byte(vector.Relaxation)) {
				t.Fatalf("relaxationBase64 = %v", vector.RelaxationBase64)
			}
			request := EraseRequest{Targets: fixture.Declared}
			effective, err := relaxedRequest(request, vector.Relaxation)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(effective.Targets, vector.Targets) {
				t.Fatalf("targets = %+v, want %+v", effective.Targets, vector.Targets)
			}
			coverage := []int64{}
			for _, target := range effective.Targets {
				coverage = append(coverage, EraseCoverageBytes(target))
			}
			if !reflect.DeepEqual(coverage, vector.Coverage) {
				t.Fatalf("coverage = %v, want %v", coverage, vector.Coverage)
			}
		})
	}
	for _, encoded := range fixture.Refused {
		for _, relaxation := range []string{lifecycle.EraseMetadataClear, lifecycle.EraseRetain} {
			if encoded == base64.StdEncoding.EncodeToString([]byte(relaxation)) {
				t.Fatalf("refused vector %q is the encoding Go sends for %s", encoded, relaxation)
			}
		}
	}
}
