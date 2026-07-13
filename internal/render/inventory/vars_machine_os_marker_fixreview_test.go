package inventory

import "testing"

func TestMarkerHashStableAcrossProxyCredentialsDir(t *testing.T) {
	hash := func(credentialsPath string) string {
		osInstall := map[string]any{
			"installer": map[string]any{
				"proxy": map[string]any{"credentialsPath": credentialsPath},
			},
		}
		return machineOSInstallMarkerVars(osInstall, "cluster", "machine", "profile")["desiredHash"].(string)
	}
	a := hash("/runs/history/run-a/tasks/t1/artifacts/runtime/secrets/proxy-credentials")
	b := hash("/runs/history/run-b/tasks/t2/artifacts/runtime/secrets/proxy-credentials")
	if a != b {
		t.Fatalf("marker hash changed across per-run proxy credentials dir: %s vs %s", a, b)
	}
	if c := hash("/runs/history/run-a/tasks/t1/artifacts/runtime/secrets/other-credentials"); c == a {
		t.Fatal("marker hash did not change when the proxy credentials basename changed")
	}
}
