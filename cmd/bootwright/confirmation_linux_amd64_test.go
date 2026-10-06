//go:build linux && amd64

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// An interactive process gives its confirmer the invocation's own arguments,
// so a confirmation refused for want of a terminal names exactly the command
// the operator ran, its stage selection or Machine selection included, with
// --yes. The process runs with standard input that is not a terminal.
func TestAnInteractiveProcessRepeatsItsInvocationWhenItsConfirmationIsRefused(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_CONFIRMATION_HELPER") == "1" {
		for _, invocation := range []struct {
			action string
			args   []string
		}{
			{"apply", []string{"apply", "--stage", "infra-components", "--authorize", "data-loss"}},
			{"trust", []string{"machine", "trust", "--machines", "rhel-01", "--replace", "rhel-01"}},
		} {
			process, _ := interactiveProcess(cli.ClassifyInvocation(invocation.args), invocation.args, io.Discard, io.Discard, controller.Route{})
			for _, reported := range diagnostics.Of(process.Confirmer.Confirm(context.Background(), invocation.action, "lab")) {
				fmt.Println(reported.Remediation)
			}
		}
		os.Exit(0)
	}
	helper := exec.Command(os.Args[0], "-test.run=^TestAnInteractiveProcessRepeatsItsInvocationWhenItsConfirmationIsRefused$")
	helper.Env = append(os.Environ(), "BOOTWRIGHT_CONFIRMATION_HELPER=1")
	output, err := helper.Output()
	want := "review the plan, then repeat bootwright apply --context lab --authorize data-loss --stage infra-components with --yes\n" +
		"review the plan, then repeat bootwright machine trust --context lab --machines rhel-01 --replace rhel-01 with --yes\n"
	if err != nil || string(output) != want {
		t.Fatalf("the refused confirmations named %q (%v), want %q", strings.TrimSpace(string(output)), err, want)
	}
}
