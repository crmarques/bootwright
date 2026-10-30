package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// The uninstall plan names each action by its identifier, dnf's preview
// verbatim and what stays, and a declined prompt refuses under the
// uninstall's own code with the command repeated with --yes.
func TestAnUninstallPlanAndPromptNameWhatTheyRemove(t *testing.T) {
	var out bytes.Buffer
	presenter := NewControllerPresenter(WithPresentation(&out, PresentationOptions{Format: "text"}), io.Discard, nil)
	err := presenter.PresentUninstallPlan(context.Background(), prerequisites.UninstallPlan{
		Actions: []prerequisites.UninstallAction{
			{ID: prerequisites.RemoveHostPackagesAction, Text: "Remove the host packages install added: podman"},
			{ID: prerequisites.RemoveStateAction, Text: "Remove Bootwright's state on this host: /var/lib/bootwright"},
		},
		Preview: &prerequisites.HostPackagePreview{Text: "Removing:\n podman  x86_64  5:5.6.1-1.fc44\n"},
		Kept:    []string{"openssh-clients (present before bootwright)", "the bootwright executable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Controller uninstall", "dnf preview", " podman  x86_64  5:5.6.1-1.fc44",
		"remove-host-packages: Remove the host packages install added: podman", "remove-state: ", "Stays on this host", "openssh-clients (present before bootwright)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the plan lacks %q:\n%s", want, out.String())
		}
	}
	answers := strings.NewReader("n\n")
	read := func(_ context.Context, p []byte) (int, error) { return answers.Read(p) }
	var prompt bytes.Buffer
	refusal := NewConfirmation(read, &prompt, func() (bool, error) { return true, nil }).Repeating([]string{"controller", "uninstall", "--keep-packages"}).
		Confirm(context.Background(), "controller uninstall", "this host")
	reported := diagnostics.Of(refusal)
	if prompt.String() != "Confirm controller uninstall on this host? [y/N] " || len(reported) != 1 || reported[0].Code != "controller.uninstall" ||
		!strings.Contains(reported[0].Remediation, "controller uninstall --keep-packages") || !strings.Contains(reported[0].Remediation, "--yes") {
		t.Fatalf("prompt %q, refusal %+v", prompt.String(), reported)
	}
}
