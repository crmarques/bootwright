package architecture_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every request golden that freezes a machine port carries the access its
// controller declares, exactly as the roles derived it from the controller
// before Go froze it: the endpoint, the transport verification, and a trust
// bundle variable only for a controller that names a bundle.
func TestEveryFrozenMachinePortCarriesItsControllersAccess(t *testing.T) {
	found := 0
	err := filepath.WalkDir("../../internal", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".golden") || !strings.Contains(path, "testdata") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var document any
		if json.Unmarshal(data, &document) != nil {
			return nil
		}
		found += checkMachinePorts(t, path, document)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found < 40 {
		t.Fatalf("only %d frozen machine ports were found in the request goldens", found)
	}
}

func checkMachinePorts(t *testing.T, path string, document any) int {
	found := 0
	switch value := document.(type) {
	case []any:
		for _, item := range value {
			found += checkMachinePorts(t, path, item)
		}
	case map[string]any:
		if port, ok := value["machinePort"].(map[string]any); ok {
			found++
			checkMachinePort(t, path, value, port)
		}
		for _, item := range value {
			found += checkMachinePorts(t, path, item)
		}
	}
	return found
}

func checkMachinePort(t *testing.T, path string, holder, port map[string]any) {
	controller, _ := holder["controller"].(map[string]any)
	text := func(object map[string]any, key string) string { found, _ := object[key].(string); return found }
	switch {
	case controller == nil:
		t.Errorf("%s: a machine port is frozen beside no controller", path)
	case port["endpoint"] != controller["endpoint"] || port["verify"] != controller["tlsVerify"]:
		t.Errorf("%s: machine port %v does not reach controller %v", path, port, controller)
	case text(port, "verification") != text(controller, "tlsVerification"):
		t.Errorf("%s: machine port %v verifies otherwise than controller %v", path, port, controller)
	case (text(port, "caVariable") != "") != (text(controller, "trustBundleRef") != ""):
		t.Errorf("%s: machine port %v names a trust bundle otherwise than controller %v", path, port, controller)
	case !strings.HasPrefix(text(port, "userVariable"), "controllerUser"):
		t.Errorf("%s: machine port %v names no controller account", path, port)
	}
}
