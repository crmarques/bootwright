package agentinstall

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

// installManifests admits only compiler-owned install-time objects.
func installManifests(catalog api.Catalog, cluster api.Object, timeSources []string) ([]InstallManifest, error) {
	result := []InstallManifest{}
	if config := cluster.Spec().Get("networking", "ovnKubernetesConfig"); config.Present() && config.Len() != 0 {
		result = append(result, InstallManifest{File: "bootwright-00-ovn-network.yaml", APIVersion: "operator.openshift.io/v1", Kind: "Network", Name: "cluster", Content: map[string]any{"apiVersion": "operator.openshift.io/v1", "kind": "Network", "metadata": map[string]any{"name": "cluster"}, "spec": map[string]any{"defaultNetwork": map[string]any{"type": "OVNKubernetes", "ovnKubernetesConfig": nativeValue(config)}}}})
	}
	if _, mirrored := managedservice.ImageMirror(catalog); !mirrored || len(timeSources) == 0 {
		return result, nil
	}
	for _, source := range timeSources {
		if !api.ValidLexical("host", source) {
			return nil, refusalAt("api.value", "$.spec.install.ntp", "the selected time server address "+strconv.Quote(source)+" is not a host name or IP address",
				"correct the address of the NTPServer selected by "+cluster.Identity())
		}
	}
	for _, role := range chronyRoles {
		result = append(result, chronyManifest(role, timeSources))
	}
	return result, nil
}

const (
	machineConfigVersion = "machineconfiguration.openshift.io/v1"
	machineConfigRole    = "machineconfiguration.openshift.io/role"
	chronyPath           = "/etc/chrony.conf"
	chronyDataPrefix     = "data:text/plain;charset=utf-8;base64,"
	chronyDefaults       = "driftfile /var/lib/chrony/drift\nmakestep 1.0 3\nrtcsync\nlogdir /var/log/chrony\n"
)

var (
	chronyRoles = []string{"master", "worker"}
	chronyFiles = map[string]string{"master": "bootwright-10-master-chrony.yaml", "worker": "bootwright-11-worker-chrony.yaml"}
)

func chronyName(role string) string {
	return "99-" + role + "-bootwright-chrony"
}

func chronyConfig(servers []string) string {
	var config strings.Builder
	for _, server := range servers {
		config.WriteString("server " + server + " iburst\n")
	}
	config.WriteString(chronyDefaults)
	return config.String()
}

func chronyManifest(role string, servers []string) InstallManifest {
	file := map[string]any{
		"path": chronyPath, "mode": 420, "overwrite": true,
		"contents": map[string]any{"source": chronyDataPrefix + base64.StdEncoding.EncodeToString([]byte(chronyConfig(servers)))},
	}
	content := map[string]any{
		"apiVersion": machineConfigVersion, "kind": "MachineConfig",
		"metadata": map[string]any{"name": chronyName(role), "labels": map[string]any{machineConfigRole: role}},
		"spec":     map[string]any{"config": map[string]any{"ignition": map[string]any{"version": "3.2.0"}, "storage": map[string]any{"files": []any{file}}}},
	}
	return InstallManifest{File: chronyFiles[role], APIVersion: machineConfigVersion, Kind: "MachineConfig", Name: chronyName(role), Content: content}
}

func chronyServers(content map[string]any) ([]string, bool) {
	spec, _ := content["spec"].(map[string]any)
	config, _ := spec["config"].(map[string]any)
	storage, _ := config["storage"].(map[string]any)
	files, _ := storage["files"].([]any)
	if len(files) != 1 {
		return nil, false
	}
	file, _ := files[0].(map[string]any)
	contents, _ := file["contents"].(map[string]any)
	source, _ := contents["source"].(string)
	encoded, found := strings.CutPrefix(source, chronyDataPrefix)
	if !found {
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	lines, found := strings.CutSuffix(string(decoded), chronyDefaults)
	if !found || lines == "" {
		return nil, false
	}
	var servers []string
	for _, line := range strings.Split(strings.TrimSuffix(lines, "\n"), "\n") {
		server, found := strings.CutPrefix(line, "server ")
		server, iburst := strings.CutSuffix(server, " iburst")
		if !found || !iburst || !api.ValidLexical("host", server) {
			return nil, false
		}
		servers = append(servers, server)
	}
	return servers, true
}

var manifestFilePattern = regexp.MustCompile(`^bootwright-[0-9]{2}-[a-z0-9-]+\.yaml$`)

func validateInstallManifests(manifests []InstallManifest) error {
	previous := ""
	for _, m := range manifests {
		if !manifestFilePattern.MatchString(m.File) || m.File <= previous {
			return fmt.Errorf("install-time manifest filenames must be ordered unique top-level Bootwright YAML names")
		}
		previous = m.File
		switch {
		case m.Kind == "Network" && m.APIVersion == "operator.openshift.io/v1" && m.Name == "cluster":
		case m.Kind == "MachineConfig" && m.APIVersion == machineConfigVersion:
			if err := validateChronyManifest(m); err != nil {
				return err
			}
		default:
			return fmt.Errorf("install-time manifest has unsupported kind or identity")
		}
		metadata, ok := m.Content["metadata"].(map[string]any)
		if !ok || metadata["name"] != m.Name || m.Content["apiVersion"] != m.APIVersion || m.Content["kind"] != m.Kind {
			return fmt.Errorf("install-time manifest content differs from its identity")
		}
	}
	return nil
}

func validateChronyManifest(m InstallManifest) error {
	for _, role := range chronyRoles {
		if m.Name != chronyName(role) || m.File != chronyFiles[role] {
			continue
		}
		servers, ok := chronyServers(m.Content)
		if !ok {
			return fmt.Errorf("install-time chrony MachineConfig %s names no time server in its derived form", m.Name)
		}
		got, err := canonicaljson.Encode(m.Content, canonicaljson.Bare)
		if err != nil || canonicaljson.Prove(got, chronyManifest(role, servers).Content, canonicaljson.Bare) != nil {
			return fmt.Errorf("install-time chrony MachineConfig %s differs from the derived content", m.Name)
		}
		return nil
	}
	return fmt.Errorf("install-time MachineConfig %s is not a Bootwright chrony configuration", m.Name)
}

func validateTimeManifests(manifests []InstallManifest, agent map[string]any) error {
	var roles []string
	var lists [][]string
	for _, m := range manifests {
		if m.Kind == "MachineConfig" {
			servers, _ := chronyServers(m.Content)
			roles = append(roles, m.Content["metadata"].(map[string]any)["labels"].(map[string]any)[machineConfigRole].(string))
			lists = append(lists, servers)
		}
	}
	if len(roles) == 0 {
		return nil
	}
	if !slices.Equal(roles, chronyRoles) {
		return fmt.Errorf("install-time chrony MachineConfigs must cover the master and worker pools together")
	}
	var sources []string
	listed, _ := agent["additionalNTPSources"].([]any)
	for _, source := range listed {
		text, _ := source.(string)
		sources = append(sources, text)
	}
	for _, servers := range lists {
		if !slices.Equal(servers, sources) {
			return fmt.Errorf("install-time chrony MachineConfigs name servers other than the agent's additional time sources")
		}
	}
	return nil
}
