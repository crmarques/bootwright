package cli

import (
	"io"
	"testing"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/converge"
)

func TestVerboseFlagOnEveryAnsibleCommand(t *testing.T) {
	commands := map[string]*cobra.Command{
		"preflight infra":             newScopeCheckCmd(converge.InfraScope, nil, io.Discard, io.Discard),
		"preflight clusters":          newScopeCheckCmd(converge.ClustersScope, nil, io.Discard, io.Discard),
		"preflight container-cluster": newScopeCheckCmd(converge.ContainerClusterScope, nil, io.Discard, io.Discard),
		"preflight storage-cluster":   newScopeCheckCmd(converge.StorageClusterScope, nil, io.Discard, io.Discard),
		"preflight all":               newCheckAllCmd(nil, io.Discard, io.Discard),
		"diff":                        newDiffCmd(io.Discard, io.Discard),
		"apply":                       newApplyCmd(nil, io.Discard, io.Discard),
		"destroy":                     newDestroyCmd(nil, io.Discard, io.Discard),
	}
	for name, cmd := range commands {
		flag := cmd.Flags().Lookup("verbose")
		if flag == nil {
			t.Errorf("%s is missing the --verbose flag", name)
			continue
		}
		if flag.Shorthand != "v" {
			t.Errorf("%s --verbose shorthand = %q, want %q", name, flag.Shorthand, "v")
		}
		if flag.Hidden {
			t.Errorf("%s --verbose must be visible in --help", name)
		}
	}
}

func TestVerboseNoLogExtraVarPairs(t *testing.T) {
	if pairs := converge.VerboseNoLogExtraVarPairs(false); pairs != nil {
		t.Fatalf("verbose off must emit no extra vars, got %v", pairs)
	}
	pairs := converge.VerboseNoLogExtraVarPairs(true)
	if len(pairs) != 1 || pairs[0] != "bootwright_no_log=false" {
		t.Fatalf("verbose on must disable no_log redaction, got %v", pairs)
	}
}
