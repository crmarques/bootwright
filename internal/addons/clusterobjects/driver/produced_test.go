package driver

import (
	"context"
	"errors"
	"strings"
	"testing"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestProducedPlacementLentOnlyForApplyAndNeverSerialized(t *testing.T) {
	for _, op := range []string{"apply", "observe"} {
		for _, missing := range []bool{false, true} {
			runner := &capture{}
			calls := []lifecycle.ProducedInput{}
			execution := lifecycle.Execution{ApplyOperation: "operation", ApplyRegistered: "2026-01-01T00:00:00Z", LocateTool: func(context.Context, controller.InstalledTool) (string, error) { return "/oc", nil }, LendProduced: func(_ context.Context, ref lifecycle.ProducedInput) (secrets.Material, error) {
				calls = append(calls, ref)
				if missing && ref.Kind == "StorageExport" {
					return secrets.Material{}, errors.New("export block ceph-export-export is unproved")
				}
				return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("private-entry")}), nil
			}}
			_, err := Run(context.Background(), runner, execution, op, Config{ProducedInput: lifecycle.ProducedInput{Kind: "ContainerCluster", Object: "cluster", Output: "kubeconfig"}, Placements: []co.SecretPlacement{{Produced: &co.ProducedRef{Kind: "StorageExport", Object: "export", Output: "external-details"}, ProducedKey: "external_cluster_details", Name: "external", Namespace: "storage", Type: "Opaque"}}})
			if op == "apply" && missing {
				if err == nil || len(runner.request.Materials) > 0 {
					t.Fatal("unproved export reached runner", err)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if op == "apply" {
				want = 2
			}
			if len(calls) != want || len(runner.request.Materials) != want {
				t.Fatal(calls, runner.request.Materials)
			}
			for _, value := range runner.request.MaterialValues {
				if strings.Contains(value, "private-entry") {
					t.Fatal("material entered invocation values")
				}
			}
		}
	}
}
