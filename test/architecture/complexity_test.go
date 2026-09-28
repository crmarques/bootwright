package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"testing"
)

// functionLineLimit is the length past which a production function stops
// explaining itself through its own structure. It is not a style rule: a body
// this long has phases a reader must hold in their head, and the fix is to name
// them.
const functionLineLimit = 100

// longFunctionsAwaitingSplit are the bodies that already exceed the limit. The
// list only shrinks: a new entry means a function grew past it instead of being
// split, and removing one is the whole point. Their split is item B47 in
// specs/milestones/m1.md.
func longFunctionsAwaitingSplit() []string {
	return []string{
		"internal/cli/results.go.writeResult",
		"internal/cli/runner.go.run",
		"internal/cli/validation.go.validateInvocation",
		"internal/containercluster/admission.go.Normalize",
		"internal/containercluster/admission.go.validateLocal",
		"internal/controller/ansiblelocal/runner_linux_amd64.go.runProcess",
		"internal/controller/bundlelocal/bootstrap.go.Resolve",
		"internal/controller/bundlelocal/bootstrap_linux_amd64.go.qualifyBootstrapELF",
		"internal/controller/bundlelocal/execution_linux_amd64.go.WithPython",
		"internal/controller/bundlelocal/manager.go.Prepare",
		"internal/controller/bundlelocal/manager.go.inspectFiles",
		"internal/controller/bundlelocal/tools.go.resolve",
		"internal/controller/hostlinux/files_linux_amd64.go.openPathPolicy",
		"internal/controller/prerequisites/bootstrap.go.CanonicalBootstrap",
		"internal/controller/prerequisites/native_plan.go.validateNativeShape",
		"internal/controller/prerequisites/service.go.inspect",
		"internal/controller/prerequisites/service.go.setup",
		"internal/controller/prerequisites/setup.go.prepare",
		"internal/desiredstate/compilation/decoding.go.value",
		"internal/desiredstate/compilation/schema_validation.go.validateShape",
		"internal/desiredstate/compilation/selection.go.selectResources",
		"internal/environment/selection.go.Select",
		"internal/machine/admission.go.Normalize",
		"internal/machine/admission.go.Validate",
		"internal/managedos/admission.go.Validate",
		"internal/reconciliation/ansiblerunner/process_linux_amd64.go.consume",
		"internal/secrets/localkeyring/artifacts.go.obsoleteArtifacts",
		"internal/secrets/localkeyring/format.go.canonicalValueSize",
		"internal/secrets/localkeyring/initialization.go.initialize",
		"internal/secrets/localkeyring/initialization.go.resumeInitialization",
		"internal/secrets/localkeyring/mutations.go.publish",
		"internal/secrets/material/files_linux_amd64.go.readFileParts",
		"internal/substrate/admission.go.Validate",
		"internal/workspace/contextfs/controller_records.go.validateControllerState",
		"internal/workspace/contextfs/initialization_linux_amd64.go.Reserve",
		"internal/workspace/contextfs/records.go.fitsJSON",
		"internal/workspace/contextfs/repository_linux_amd64.go.writeRegistry",
		"internal/workspace/selectionfs/files_linux_amd64.go.local",
	}
}

func TestProductionFunctionsStayWithinTheLineLimit(t *testing.T) {
	known := longFunctionsAwaitingSplit()
	var seen []string
	for _, source := range productionSources(t) {
		fileSet := token.NewFileSet()
		syntax, err := parser.ParseFile(fileSet, filepath.Join("..", "..", source.path), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range syntax.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			lines := fileSet.Position(function.Body.End()).Line - fileSet.Position(function.Body.Pos()).Line + 1
			if lines <= functionLineLimit {
				continue
			}
			name := source.path + "." + function.Name.Name
			seen = append(seen, name)
			if !slices.Contains(known, name) {
				t.Errorf("%s is %d lines; split it into named phases or add it to longFunctionsAwaitingSplit with its reason", name, lines)
			}
		}
	}
	for index, name := range known {
		if slices.Contains(known[:index], name) {
			t.Errorf("%s is listed twice in longFunctionsAwaitingSplit; remove the copy", name)
		} else if !slices.Contains(seen, name) {
			t.Errorf("%s is no longer over the limit; remove it from longFunctionsAwaitingSplit", name)
		}
	}
}
