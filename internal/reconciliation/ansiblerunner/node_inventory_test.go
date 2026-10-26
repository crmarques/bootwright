package ansiblerunner

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestNodeInventoryAliasesStayOnTheController(t *testing.T) {
	request := localRequest()
	request.NodeCount, request.Parallelism = 28, 4
	value, err := phaseInventory(request, nil)
	if err != nil {
		t.Fatal(err)
	}
	children := value["all"].(map[string]any)["children"].(map[string]any)
	hosts := children["bootwright_nodes"].(map[string]any)["hosts"].(map[string]any)
	if len(hosts) != 28 {
		t.Fatalf("node hosts=%d", len(hosts))
	}
	for _, value := range hosts {
		host := value.(map[string]any)
		if host["ansible_connection"] != "local" || host["ansible_host"] != "localhost" || host["bootwright_controller_host"] != request.Placement.Machine {
			t.Fatalf("node alias reaches another host: %v", host)
		}
	}
	if len(children["bootwright_target"].(map[string]any)["hosts"].(map[string]any)) != 1 {
		t.Fatal("controller play lost its sole host")
	}
}

func TestNodeInventoryRefusesUnboundedOrRemotePhases(t *testing.T) {
	for name, request := range map[string]lifecycle.RunRequest{"remote": sshRequest(), "count": localRequest(), "fanout": localRequest()} {
		request.NodeCount, request.Parallelism = 4, 4
		if name == "count" {
			request.NodeCount = 513
		}
		if name == "fanout" {
			request.Parallelism = 17
		}
		if _, err := phaseInventory(request, nil); err == nil {
			t.Fatalf("%s phase admitted", name)
		}
	}
}

func TestSerializedNodeInventoryPreservesFrozenNodeOrder(t *testing.T) {
	request := localRequest()
	request.NodeCount, request.Parallelism = 28, 4
	value, err := phaseInventory(request, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	previous := -1
	for index := range request.NodeCount {
		position := strings.Index(string(encoded), fmt.Sprintf("\"bootwright_node_%03d\":", index))
		if position <= previous {
			t.Fatalf("node %d appears outside frozen order in serialized inventory", index)
		}
		previous = position
	}
}
