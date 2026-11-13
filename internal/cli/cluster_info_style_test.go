package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestClusterInfoEmphasizesAccessDetails(t *testing.T) {
	for _, detail := range []string{"compact", "full"} {
		for _, width := range []int{48, 96, 220} {
			options := PresentationOptions{Color: "always", Width: width, Detail: detail}
			var colored, plain bytes.Buffer
			result := storageOnly(revealedCeph(false))
			for _, output := range []struct {
				buffer *bytes.Buffer
				color  string
			}{{&colored, "always"}, {&plain, "never"}} {
				options.Color = output.color
				if err := writeClusterInfo(WithPresentation(output.buffer, options), io.Discard, "cluster info", result, false); err != nil {
					t.Fatal(err)
				}
			}
			if ansi.Strip(colored.String()) != plain.String() {
				t.Fatalf("%s/%d: styling changed the content or layout", detail, width)
			}
			options.Color = "always"
			for _, heading := range []string{"Topology", "Access", "Evidence", "Resources", "Custody"} {
				if !strings.Contains(colored.String(), styleIdentity(heading, options)) {
					t.Errorf("%s/%d: heading %q lacks emphasis", detail, width, heading)
				}
			}
			for _, value := range []string{"admin", clusterDashboardPassword} {
				if !strings.Contains(colored.String(), styleIdentity(value, options)) {
					t.Errorf("%s/%d: credential lacks emphasis", detail, width)
				}
			}
			for _, url := range []string{"https://rhel-01.lab.test:8443/", "https://rhel-02.lab.test:8443/"} {
				if !strings.Contains(colored.String(), styleAccent(url, options)) {
					t.Errorf("%s/%d: dashboard URL lacks emphasis", detail, width)
				}
			}
			if detail == "full" && !strings.Contains(colored.String(), styleAccent("nfs://192.0.2.60:2049/", options)) {
				t.Errorf("%d: endpoint URL lacks emphasis", width)
			}
			if !strings.Contains(colored.String(), styleState("ready", options)) || !strings.Contains(colored.String(), styleState("done", options)) {
				t.Error("statuses lack semantic colors")
			}
		}
	}
}

func TestClusterInfoKeepsAccessNotesMutedAndEscapesValues(t *testing.T) {
	options := PresentationOptions{Color: "always", Width: 220}
	ceph := readyCeph()
	ceph.Dashboard.URLs[0].URL += "\x1b[2J"
	var out bytes.Buffer
	if err := writeClusterInfo(WithPresentation(&out, options), io.Discard, "cluster info", storageOnly(ceph), false); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"  (active MGR rhel-01 at last observation)", "hidden; rerun with --secrets --name storage"} {
		if !strings.Contains(out.String(), styleMuted(value, options)) {
			t.Errorf("access note %q is not muted", value)
		}
	}
	if strings.Contains(out.String(), "\x1b[2J") || !strings.Contains(ansi.Strip(out.String()), `\u001b[2J`) {
		t.Error("a URL's control character was not escaped exactly once")
	}
	if strings.Contains(out.String(), clusterDashboardPassword) {
		t.Error("ordinary inspection revealed a password")
	}
	ceph.Dashboard.Status = "unproved"
	ceph.Dashboard.Reason = "the endpoint was not proved reachable"
	out.Reset()
	if err := writeClusterInfo(WithPresentation(&out, options), io.Discard, "cluster info", storageOnly(ceph), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), styleState("unproved", options)) || !strings.Contains(out.String(), styleMuted(": "+ceph.Dashboard.Reason, options)) {
		t.Error("an unproved dashboard should emphasize its status and keep the reason muted")
	}
}
