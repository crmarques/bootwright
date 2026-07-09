package render_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/render/inventory"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func TestVSphereFixtureRendersMachineComponentAndBootVars(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "007-sno-vsphere")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	secretsDir := t.TempDir()
	vars := inventory.VarsWithSecretsDir(state, secretsDir)
	cluster := clustersByName(t, vars)["sno-vsphere"]
	machine := firstMachineComponent(t, cluster)

	if got := machine["machineRef"]; got != "localhost" {
		t.Fatalf("machineRef = %v, want localhost (vCenter operations run on the controller)", got)
	}
	vsphere, ok := machine["vsphere"].(map[string]any)
	if !ok {
		t.Fatalf("machine component missing vsphere block: %v", machine)
	}
	if got := vsphere["server"]; got != "vcenter.bootwright.test" {
		t.Fatalf("vsphere server = %v", got)
	}
	if got := vsphere["credentialsRef"]; got != "vcenter-credentials" {
		t.Fatalf("vsphere credentialsRef = %v", got)
	}
	if got := vsphere["credentialsPath"]; got != filepath.Join(secretsDir, "vcenter-credentials") {
		t.Fatalf("vsphere credentialsPath = %v, want %s", got, filepath.Join(secretsDir, "vcenter-credentials"))
	}
	if got := vsphere["disableCertificateVerification"]; got != true {
		t.Fatalf("vsphere disableCertificateVerification = %v, want true", got)
	}
	if got := vsphere["failureDomain"]; got != "dc1-zone-a" {
		t.Fatalf("vsphere failureDomain = %v", got)
	}
	topology := vsphere["topology"].(map[string]any)
	if got := topology["datastore"]; got != "datastore1" {
		t.Fatalf("vsphere topology datastore = %v, want the object name", got)
	}
	if got := topology["computeCluster"]; got != "cluster1" {
		t.Fatalf("vsphere topology computeCluster = %v, want the object name", got)
	}
	if got := topology["resourcePool"]; got != "bootwright" {
		t.Fatalf("vsphere topology resourcePool = %v, want the object name", got)
	}
	if got := topology["folder"]; got != "/dc1/vm/bootwright" {
		t.Fatalf("vsphere topology folder = %v, want the authored path", got)
	}
	staging := vsphere["isoStaging"].(map[string]any)
	if got := staging["datastore"]; got != "datastore1" {
		t.Fatalf("isoStaging datastore = %v, want the failure-domain datastore name", got)
	}
	if got := staging["folder"]; got != "bootwright-vmedia" {
		t.Fatalf("isoStaging folder = %v", got)
	}

	boot, ok := machine["boot"].(map[string]any)
	if !ok {
		t.Fatalf("machine component missing boot block: %v", machine)
	}
	readiness := boot["readiness"].(map[string]any)
	if got := readiness["type"]; got != "ssh" {
		t.Fatalf("boot readiness type = %v, want ssh", got)
	}
	agentISO := boot["agentIso"].(map[string]any)
	if got := agentISO["stageHost"]; got != "localhost" {
		t.Fatalf("agentIso stageHost = %v, want localhost", got)
	}
	stagePath, _ := agentISO["stagePath"].(string)
	wantStage := "{{ bootwright_provider_state_dir }}/vsphere/lab-vsphere-provider/vmedia/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-vsphere.iso"
	if stagePath != wantStage {
		t.Fatalf("agentIso stagePath = %q, want %q", stagePath, wantStage)
	}
	fetchURL, _ := agentISO["fetchUrl"].(string)
	wantFetch := "[datastore1] bootwright-vmedia/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-vsphere.iso"
	if fetchURL != wantFetch {
		t.Fatalf("agentIso fetchUrl = %q, want %q", fetchURL, wantFetch)
	}

	if targets, _ := cluster["agentIsoPublishTargets"].([]any); len(targets) != 0 {
		t.Fatalf("agentIsoPublishTargets = %v, want empty for vsphere machines", targets)
	}

	interfaces := machine["interfaces"].([]any)
	mac, _ := interfaces[0].(map[string]any)["macAddress"].(string)
	if !strings.HasPrefix(mac, "00:50:56:") {
		t.Fatalf("generated vSphere MAC = %q, want the VMware manual-assignment prefix", mac)
	}
	fourth, err := strconv.ParseUint(strings.Split(mac, ":")[3], 16, 8)
	if err != nil || fourth > 0x3f {
		t.Fatalf("generated vSphere MAC %q fourth octet must stay within 00..3f", mac)
	}

	agent, err := render.AgentConfig(state, containerClusterByName(t, state, "sno-vsphere"))
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	agentIface := agent["hosts"].([]any)[0].(map[string]any)["interfaces"].([]any)[0].(map[string]any)
	if got := agentIface["macAddress"]; got != mac {
		t.Fatalf("agent-config MAC = %v, want vars MAC %s", got, mac)
	}

	attachment := machine["networkAttachment"].(map[string]any)
	if got := attachment["kind"]; got != v1alpha1.ProvisionerVSphere {
		t.Fatalf("network attachment kind = %v, want vsphere", got)
	}
}
