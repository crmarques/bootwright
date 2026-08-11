package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestHAProxyTaskBoundariesPreserveConfigRecreate(t *testing.T) {
	tasks := readAnsibleTasks(t, bootwrightCollectionRoleRoot+"/infra_component_load_balancer_haproxy/tasks/main.yml")

	render := tasks[findAnsibleTask(t, tasks, "Render HAProxy configuration")]
	if got := render["register"]; got != "bootwright_haproxy_config" {
		t.Fatalf("HAProxy config render register = %v, want bootwright_haproxy_config", got)
	}

	containerTask := tasks[findAnsibleTask(t, tasks, "Run HAProxy container")]
	container, ok := containerTask["containers.podman.podman_container"].(map[string]any)
	if !ok {
		t.Fatalf("Run HAProxy container has no podman_container body: %v", containerTask)
	}
	if got := container["recreate"]; got != "{{ bootwright_haproxy_config.changed }}" {
		t.Fatalf("HAProxy container recreate = %v, want rendered-config change result", got)
	}
	if got := fmt.Sprint(container["volumes"]); !strings.Contains(got, "haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro,Z") {
		t.Fatalf("HAProxy container volume does not mount its rendered config: %v", container["volumes"])
	}
	command := fmt.Sprint(container["command"])
	for _, want := range []string{"haproxy", "-f", "/usr/local/etc/haproxy/haproxy.cfg"} {
		if !strings.Contains(command, want) {
			t.Fatalf("HAProxy container command missing %q: %v", want, container["command"])
		}
	}

	firewallGate := tasks[findAnsibleTask(t, tasks, "Revalidate host operation and endpoints before HAProxy firewall mutation")]
	include, ok := firewallGate["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("HAProxy firewall revalidation has no include_role body: %v", firewallGate)
	}
	for _, invalid := range []string{"volumes", "command"} {
		if _, ok := include[invalid]; ok {
			t.Fatalf("HAProxy firewall include_role owns container option %q: %v", invalid, include[invalid])
		}
	}
}
