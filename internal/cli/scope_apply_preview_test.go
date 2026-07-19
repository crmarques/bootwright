package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestNoteIneffectiveAllowDestroy(t *testing.T) {
	cases := []struct {
		name        string
		allow       bool
		dryRun      bool
		destructive []string
		want        string
	}{
		{"real no-op", true, false, nil, "had no effect"},
		{"dry-run", true, true, nil, "not consumed by a dry-run"},
		{"real with descriptors stays silent", true, false, []string{"Machine/db1"}, ""},
		{"absent stays silent", false, false, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			noteIneffectiveAllowDestroy(&out, tc.allow, tc.dryRun, tc.destructive)
			if tc.want == "" {
				if out.Len() != 0 {
					t.Fatalf("expected no notice, got %q", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("notice must contain %q, got %q", tc.want, out.String())
			}
		})
	}
}

func TestPrintApplyTransitionLedgerPromotesReinstall(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := planApplyTasks(t, converge.AllScope.ApplyTarget(), state)
	runsDir := t.TempDir()

	var out bytes.Buffer
	printApplyTransitionLedger(&out, tasks, runsDir, workflow.ApplyModeOverride, []string{"sno-libvirt"})
	got := out.String()
	if !strings.Contains(got, "DESTROY & rebuild") || !strings.Contains(got, "ContainerCluster/sno-libvirt") {
		t.Fatalf("a reinstall-input-drifted cluster must show under DESTROY & rebuild, got %q", got)
	}

	out.Reset()
	printApplyTransitionLedger(&out, tasks, runsDir, workflow.ApplyModeOverride, nil)
	if strings.Contains(out.String(), "DESTROY & rebuild") {
		t.Fatalf("without a reinstall flag the cluster must not show DESTROY & rebuild, got %q", out.String())
	}
}

func TestPrintArtifactServerReclaimNotice(t *testing.T) {
	var out bytes.Buffer
	printArtifactServerReclaimNotice(&out, []string{"InfraComponent-artifact"})
	got := out.String()
	if !strings.Contains(got, "reclaim install-only artifact server") || !strings.Contains(got, "InfraComponent-artifact") {
		t.Fatalf("reclaim notice must name the server, got %q", got)
	}
	out.Reset()
	printArtifactServerReclaimNotice(&out, nil)
	if out.Len() != 0 {
		t.Fatalf("no reclaim targets must print nothing, got %q", out.String())
	}
}
