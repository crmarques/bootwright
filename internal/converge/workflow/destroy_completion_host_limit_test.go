package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func TestDestroyTasksBindCompletionToExactExecutedPlayHosts(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sawFannedMachine := false
	for _, task := range tasks {
		want := ""
		switch {
		case task.Entry.Kind == DestroyTaskKindControllerNameResolution:
			want = render.GroupControllerHosts
		case task.Entry.Kind == DestroyTaskKindContainerCluster || task.Entry.Kind == DestroyTaskKindContainerClusterRuntime:
			want = render.GroupOCPHosts
		case task.Entry.ID == destroyMachineInfraRecordsTaskID:
			want = render.GroupProviderHosts + ":" + render.GroupInfraHosts
		case task.Entry.ID == destroyMachineInfraTaskID:
			want = machineInfraDestroyLimit()
		case strings.HasPrefix(task.Entry.ID, destroyMachineInfraTaskID+"."):
			cluster := strings.TrimPrefix(task.Entry.ID, destroyMachineInfraTaskID+".")
			want = destroyMachineInfraClusterGroup(state, cluster) + ":" + render.GroupProviderHosts + ":" + render.GroupInfraHosts
			sawFannedMachine = true
		case task.Entry.Kind == DestroyTaskKindInfraComponents:
			want = render.GroupInfraComponentHosts
		case task.Entry.Kind == DestroyTaskKindProviderServices:
			want = render.GroupProviderHosts
		case task.Entry.Kind == DestroyTaskKindStorageCluster:
			want = completionStorageHostLimit(task.Entry.ID, DestroyStorageClustersTaskID)
		case task.Entry.Kind == DestroyTaskKindMachineRegistration:
			want = completionStorageHostLimit(task.Entry.ID, destroyMachineRegistrationTaskID)
		case task.Entry.Kind == DestroyTaskKindStorageNodeAccess:
			want = completionStorageHostLimit(task.Entry.ID, destroyStorageNodeAccessTaskID)
		default:
			t.Fatalf("registered destroy task %q kind %q has no expected completion play-host mapping", task.Entry.ID, task.Entry.Kind)
		}
		if task.CompletionHostLimit != want {
			t.Errorf("task %q completion host limit = %q, want exact executed play hosts %q", task.Entry.ID, task.CompletionHostLimit, want)
		}
	}
	if !sawFannedMachine {
		t.Fatal("fixture planned no fanned machine task; provider and infra union coverage would pass vacuously")
	}
}

func completionStorageHostLimit(id, base string) string {
	if id == base {
		return render.GroupStorageHosts
	}
	cluster := strings.TrimPrefix(id, base+".")
	if cluster == id || cluster == "" {
		return ""
	}
	return render.StorageClusterGroupName(cluster)
}
