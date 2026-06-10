package installer_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"

	installerrender "github.com/crmarques/bootwright/internal/render/installer"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

const (
	testPullSecret = `{"auths":{"cloud.openshift.com":{"auth":"dXNlcjpwYXNz","email":"test@example.com"}}}`
	testSSHKey     = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBfJDIran72ilARpsCvkV3JcwutN7lA7vK3bfYTyyATL"
)

var installerValidationFixtures = []string{
	"001-sno-libvirt",
	"002-sno-emul-baremetal",
	"003-3nodes-libvirt",
	"004-3nodes-emul-baremetal",
	"005-3nodes-baremetal",
}

func TestRenderedInstallerInputsPassOpenShiftInstallValidation(t *testing.T) {
	installer, err := exec.LookPath("openshift-install")
	if err != nil {
		t.Skip("openshift-install not available")
	}
	for _, fixture := range installerValidationFixtures {
		t.Run(fixture, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, fixture)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			for _, ocp := range state.ContainerClusters {
				dir := t.TempDir()
				installConfig, err := installerrender.InstallerConfigWithSecrets(state, ocp, installerrender.InstallerSecrets{
					PullSecret: testPullSecret,
					SSHKey:     testSSHKey,
				})
				if err != nil {
					t.Fatalf("InstallerConfigWithSecrets: %v", err)
				}
				agentConfig, err := installerrender.AgentConfig(state, ocp)
				if err != nil {
					t.Fatalf("AgentConfig: %v", err)
				}
				writeTestYAML(t, filepath.Join(dir, "install-config.yaml"), installConfig)
				writeTestYAML(t, filepath.Join(dir, "agent-config.yaml"), agentConfig)

				cmd := exec.Command(installer, "agent", "create", "cluster-manifests", "--dir", dir, "--log-level", "debug")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("openshift-install validation failed:\n%s", out)
				}
			}
		})
	}
}

func TestRenderedAgentNetworkConfigsPassNmstateValidation(t *testing.T) {
	nmstate, err := exec.LookPath("nmstatectl")
	if err != nil {
		t.Skip("nmstatectl not available")
	}
	for _, fixture := range installerValidationFixtures {
		t.Run(fixture, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, fixture)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			for _, ocp := range state.ContainerClusters {
				agentConfig, err := installerrender.AgentConfig(state, ocp)
				if err != nil {
					t.Fatalf("AgentConfig: %v", err)
				}
				hosts := agentConfig["hosts"].([]any)
				for _, rawHost := range hosts {
					host := rawHost.(map[string]any)
					networkConfig, ok := host["networkConfig"].(map[string]any)
					if !ok {
						continue
					}
					data, err := yaml.Marshal(networkConfig)
					if err != nil {
						t.Fatalf("marshal host networkConfig: %v", err)
					}
					cmd := exec.Command(nmstate, "gc", "-")
					cmd.Stdin = bytes.NewReader(data)
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("nmstatectl validation failed for host %v:\n%s\ninput:\n%s", host["hostname"], out, data)
					}
				}
			}
		})
	}
}

func writeTestYAML(t *testing.T, path string, value any) {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
