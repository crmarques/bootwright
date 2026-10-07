package architecture_test

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// documentedBound ties one bound a spec states to the Go code that enforces
// it. The phrase is the spec text around the value, with %s where the value
// appears, and it must occur exactly once. The constant names a package-level
// constant of source. A duration is a deadline, and applied says where it
// takes effect; a count or a size in bytes names nothing there.
type documentedBound struct {
	spec, phrase, source, constant string
	applied                        *application
	size                           bool
}

// application names the function that passes a deadline to
// context.WithTimeout, in the source that declares it when that is not the
// bound's own. The function passes the constant itself, or a call to a
// function of that source which returns the constant for a request that states
// no deadline of its own. A ceiling is instead what that function clamps a
// stated deadline to with min, so no request's deadline passes it.
type application struct {
	runner, source string
	ceiling        bool
}

func documentedBounds() []documentedBound {
	const (
		contexts         = "specs/contexts.md"
		store            = "internal/workspace/contextfs/store.go"
		bundles          = "internal/workspace/contextfs/controller_records.go"
		setupRuns        = "internal/workspace/contextfs/controller_runs_linux_amd64.go"
		boundedRuns      = "internal/workspace/contextfs/runs_linux_amd64.go"
		operations       = "internal/reconciliation/operationstore/records.go"
		lifecycleRun     = "internal/reconciliation/ansiblerunner/process_linux_amd64.go"
		lifecycleRequest = "internal/reconciliation/lifecycle/invocation.go"
		setupRun         = "internal/controller/ansiblelocal/runner_linux_amd64.go"
		mediaRecords     = "internal/managedos/media.go"
		mediaStore       = "internal/workspace/contextfs/media_linux_amd64.go"
		commands         = "specs/cli/commands.md"
		completion       = "cmd/bootwright/completion_paths.go"
	)
	return []documentedBound{
		{contexts, "| Active or reserved context names | %s |", store, "maxContexts", nil, false},
		{contexts, "The %s-name bound applies", store, "maxContexts", nil, false},
		{contexts, "| Revisions per context | %s |", store, "maxRevisions", nil, false},
		{contexts, "| Retained [controller bundle namespaces](contexts/controller-record.md#bounds) | %s |", bundles, "maxControllerBundles", nil, false},
		{"specs/contexts/controller-record.md", "There are at most %s retained bundle", bundles, "maxControllerBundles", nil, false},
		{contexts, "| [Setup runs](contexts/controller-record.md#setup-runs) the controller directory keeps | %s |", setupRuns, "maxSetupRuns", nil, false},
		{"specs/contexts/controller-record.md", "There are at most %s runs:", setupRuns, "maxSetupRuns", nil, false},
		{"specs/contexts/controller-record.md", "`run.output`, which is bounded at %s.", setupRuns, "maxSetupRunOutput", nil, true},
		{contexts, "| [Bounded runs](cli/output.md#bounded-run-output) one context's runs area keeps | %s |", boundedRuns, "maxBoundedRuns", nil, false},
		{contexts, "| Lifecycle operations one context retains | %s |", operations, "MaxOperations", nil, false},
		{contexts, "| Entries in one context's lifecycle operation area, and in each of its runs and SSH-trust areas | %s |", operations, "MaxEntries", nil, false},
		{contexts, "and must still keep %s entries free", operations, "ReservedEntries", nil, false},
		{contexts, "| Bytes in one context's lifecycle operation area, and in each of its runs and SSH-trust areas | %s |", operations, "MaxBytes", nil, true},
		{contexts, "fresh apply needs %s of them free", operations, "ReservedBytes", nil, true},
		{contexts, "| One lifecycle adapter invocation whose request states no deadline | %s |", lifecycleRun, "invocationTimeout", &application{runner: "execute"}, false},
		{contexts, "| The longest deadline a lifecycle adapter request may state | %s |", lifecycleRequest, "MaxDeadline", &application{runner: "execute", source: lifecycleRun, ceiling: true}, false},
		{contexts, "| One controller Ansible run: setup, its recovery or the base of a controller-stage client installation | %s |", setupRun, "runTimeout", &application{runner: "runProcess"}, false},
		{contexts, "| The longest deadline a controller-stage client installation may run under | %s |", setupRun, "clientStageCeiling", &application{runner: "runProcess", ceiling: true}, false},
		{"specs/container-clusters.md", "whose `minSizeGigabytes` exceeds %s", "internal/containercluster/agentinstall/selection.go", "maxInstallerRootDeviceGigabytes", nil, false},
		{contexts, "| Bytes in one installer media image | %s |", mediaRecords, "MaxMediaBytes", nil, true},
		{contexts, "| Installer media images one host holds | %s |", mediaRecords, "MaxMediaEntries", nil, false},
		{contexts, "| Bytes in an installer media name | %s |", mediaRecords, "MaxMediaName", nil, false},
		{contexts, "| Installer media stages at once, live, retained or abandoned | %s |", mediaStore, "maxStagedMedia", nil, false},
		{contexts, "At most %s stages exist at once", mediaStore, "maxStagedMedia", nil, false},
		{"specs/cli/commands.md", "basename of 5 through %s bytes", mediaRecords, "MaxMediaName", nil, false},
		{"specs/managed-os.md", "an origin of at most %s bytes", mediaRecords, "MaxMediaOrigin", nil, false},
		{"specs/cli/commands.md", "an origin over %s bytes", mediaRecords, "MaxMediaOrigin", nil, false},
		{"specs/managed-os.md", "a transfer deadline of %s", "internal/managedos/medialocal/acquirer.go", "transferTimeout", &application{runner: "openURL"}, false},
		{commands, "It reads at most %s of those entries", completion, "maxCompletionEntriesRead", nil, false},
		{commands, "stops once it holds %s candidates", completion, "maxCompletionPaths", nil, false},
		{commands, "A prefix longer than %s bytes", completion, "maxCompletionPrefix", nil, false},
		{commands, "an entry whose name is longer than %s bytes", completion, "maxCompletionEntry", nil, false},
		{"specs/controller.md", "each entry at most %s bytes", "api/v1alpha1/lexical.go", "maxProxyBypassBytes", nil, false},
	}
}

func TestDocumentedBoundsMatchCode(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, bound := range documentedBounds() {
		t.Run(bound.constant+" in "+filepath.Base(bound.spec), func(t *testing.T) {
			spec, err := os.ReadFile(filepath.Join(root, bound.spec))
			if err != nil {
				t.Fatal(err)
			}
			syntax, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, bound.source), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			value, duration := boundValue(t, boundExpression(t, syntax, bound.source, bound.constant))
			if duration && bound.size {
				t.Fatalf("%s: a duration is not a size in bytes", bound.constant)
			}
			phrase := fmt.Sprintf(bound.phrase, renderBound(t, value, duration, bound.size))
			if count := strings.Count(string(spec), phrase); count != 1 {
				t.Errorf("%s states %q %d times, want once: %s in %s changed, or the spec did", bound.spec, phrase, count, bound.constant, bound.source)
			}
			if duration != (bound.applied != nil) {
				t.Fatalf("%s: a duration names the function that applies it, and a count names none", bound.constant)
			}
			if bound.applied != nil {
				if problem := appliedDeadline(t, root, bound, syntax); problem != "" {
					t.Error(problem)
				}
			}
		})
	}
}

// appliedDeadline reports how the runner fails to apply a deadline bound, or
// nothing when it applies it as the bound says.
func appliedDeadline(t *testing.T, root string, bound documentedBound, declared *ast.File) string {
	t.Helper()
	source, syntax, name := bound.source, declared, bound.constant
	if bound.applied.source != "" && bound.applied.source != bound.source {
		source = bound.applied.source
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, source), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		syntax, name = parsed, importedName(t, parsed, source, bound.source)+"."+bound.constant
	}
	runner := bound.applied.runner
	deadline := runnerDeadline(t, syntax, source, runner)
	if !bound.applied.ceiling && types.ExprString(deadline) == name {
		return ""
	}
	var function *ast.Ident
	if call, ok := deadline.(*ast.CallExpr); ok {
		function, _ = call.Fun.(*ast.Ident)
	}
	if function == nil {
		return fmt.Sprintf("%s in %s passes %s to context.WithTimeout, want %s or a call to a function of that source applying it", runner, source, types.ExprString(deadline), name)
	}
	for _, returned := range returnedExpressions(t, syntax, source, function.Name) {
		if !bound.applied.ceiling && types.ExprString(returned) == name {
			return ""
		}
		if clamp, ok := returned.(*ast.CallExpr); ok && bound.applied.ceiling && types.ExprString(clamp.Fun) == "min" {
			for _, argument := range clamp.Args {
				if types.ExprString(argument) == name {
					return ""
				}
			}
		}
	}
	if bound.applied.ceiling {
		return fmt.Sprintf("%s in %s never clamps the deadline %s passes to context.WithTimeout to %s with min", function.Name, source, runner, name)
	}
	return fmt.Sprintf("%s in %s never returns %s for the deadline %s passes to context.WithTimeout", function.Name, source, name, runner)
}

// deadlineProof is the test each capability that states its run's deadline
// declares in its own package: the deadline covers every budget its frozen
// request carries and stays within the ceiling the runner holds it to.
const deadlineProof = "TestEveryCapabilityDeadlineCoversItsFrozenBudgets"

// A capability states its run's deadline through the Deadline of the
// invocation it builds. Each package that does declares deadlineProof, so a
// capability cannot start stating a deadline without proving it covers what
// its request waits for; this suite reads sources and imports none of them.
func TestEveryCapabilityStatingADeadlineProvesIt(t *testing.T) {
	const invocation = "github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	stating := map[string]bool{}
	for _, source := range productionSources(t) {
		for _, imported := range source.imports {
			if imported.path != invocation {
				continue
			}
			ast.Inspect(source.syntax, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || !isSelector(literal.Type, imported.alias, "Invocation") {
					return true
				}
				for _, element := range literal.Elts {
					if field, ok := element.(*ast.KeyValueExpr); ok && types.ExprString(field.Key) == "Deadline" {
						stating[source.owner] = true
					}
				}
				return true
			})
		}
	}
	if !stating["internal/managedos/installation"] {
		t.Fatalf("no invocation states a deadline in internal/managedos/installation; found %v, so the walk stopped seeing them", stating)
	}
	for owner := range stating {
		tests, err := filepath.Glob(filepath.Join("..", "..", owner, "*_test.go"))
		if err != nil {
			t.Fatal(err)
		}
		declared := false
		for _, path := range tests {
			syntax, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, declaration := range syntax.Decls {
				if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil && function.Name.Name == deadlineProof {
					declared = true
				}
			}
		}
		if !declared {
			t.Errorf("%s states its run's deadline but declares no %s", owner, deadlineProof)
		}
	}
}

// importedName is the name a source refers to the package declaring a bound by.
func importedName(t *testing.T, syntax *ast.File, source, declaring string) string {
	t.Helper()
	want := "github.com/crmarques/bootwright/" + filepath.ToSlash(filepath.Dir(declaring))
	for _, imported := range syntax.Imports {
		if path, err := strconv.Unquote(imported.Path.Value); err == nil && path == want {
			if imported.Name != nil {
				return imported.Name.Name
			}
			return filepath.Base(path)
		}
	}
	t.Fatalf("%s does not import %s, which declares the bound it applies", source, want)
	return ""
}

// returnedExpressions lists every expression one function of a source returns.
func returnedExpressions(t *testing.T, syntax *ast.File, source, function string) []ast.Expr {
	t.Helper()
	var found []ast.Expr
	declared := false
	for _, declaration := range syntax.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if !ok || candidate.Recv != nil || candidate.Name.Name != function || candidate.Body == nil {
			continue
		}
		declared = true
		ast.Inspect(candidate.Body, func(node ast.Node) bool {
			if _, literal := node.(*ast.FuncLit); literal {
				return false
			}
			if statement, ok := node.(*ast.ReturnStmt); ok {
				found = append(found, statement.Results...)
			}
			return true
		})
	}
	if !declared {
		t.Fatalf("%s declares no function %s", source, function)
	}
	return found
}

// boundExpression finds the value of the package-level constant that defines a
// bound.
func boundExpression(t *testing.T, syntax *ast.File, source, name string) ast.Expr {
	t.Helper()
	var found []ast.Expr
	for _, declaration := range syntax.Decls {
		declared, ok := declaration.(*ast.GenDecl)
		if !ok || declared.Tok != token.CONST {
			continue
		}
		for _, spec := range declared.Specs {
			value := spec.(*ast.ValueSpec)
			for index, identifier := range value.Names {
				if identifier.Name == name && index < len(value.Values) {
					found = append(found, value.Values[index])
				}
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s defines %s %d times, want once", source, name, len(found))
	}
	return found[0]
}

// runnerDeadline finds the one deadline a function passes to
// context.WithTimeout.
func runnerDeadline(t *testing.T, syntax *ast.File, source, function string) ast.Expr {
	t.Helper()
	var found []ast.Expr
	for _, declaration := range syntax.Decls {
		declared, ok := declaration.(*ast.FuncDecl)
		if !ok || declared.Name.Name != function || declared.Body == nil {
			continue
		}
		ast.Inspect(declared.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && isSelector(call.Fun, "context", "WithTimeout") && len(call.Args) == 2 {
				found = append(found, call.Args[1])
			}
			return true
		})
	}
	if len(found) != 1 {
		t.Fatalf("%s in %s calls context.WithTimeout %d times, want once", function, source, len(found))
	}
	return found[0]
}

func isSelector(expression ast.Expr, pkg, name string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == pkg
}

// boundValue folds a constant expression of literals, time units and
// arithmetic, and reports whether it is a duration.
func boundValue(t *testing.T, expression ast.Expr) (constant.Value, bool) {
	t.Helper()
	units := map[string]time.Duration{
		"Nanosecond": time.Nanosecond, "Microsecond": time.Microsecond, "Millisecond": time.Millisecond,
		"Second": time.Second, "Minute": time.Minute, "Hour": time.Hour,
	}
	switch node := expression.(type) {
	case *ast.BasicLit:
		return constant.MakeFromLiteral(node.Value, node.Kind, 0), false
	case *ast.ParenExpr:
		return boundValue(t, node.X)
	case *ast.SelectorExpr:
		if unit, ok := units[node.Sel.Name]; ok && isSelector(node, "time", node.Sel.Name) {
			return constant.MakeInt64(int64(unit)), true
		}
	case *ast.BinaryExpr:
		x, xDuration := boundValue(t, node.X)
		y, yDuration := boundValue(t, node.Y)
		if node.Op == token.SHL || node.Op == token.SHR {
			shift, ok := constant.Uint64Val(y)
			if !ok {
				t.Fatalf("shift %s is not a constant count", y)
			}
			return constant.Shift(x, node.Op, uint(shift)), xDuration
		}
		return constant.BinaryOp(x, node.Op, y), xDuration || yDuration
	}
	t.Fatalf("bound expression %T is not a supported constant expression", expression)
	return nil, false
}

// renderBound writes a value the way the specs state it: a count in decimal,
// a size in its largest whole binary unit, and a duration in the largest whole
// unit.
func renderBound(t *testing.T, value constant.Value, duration, size bool) string {
	t.Helper()
	number, exact := constant.Int64Val(value)
	if !exact {
		t.Fatalf("bound %s is not an integer", value)
	}
	if size {
		for _, unit := range []struct {
			shift uint
			name  string
		}{{30, "GiB"}, {20, "MiB"}, {10, "KiB"}} {
			if count := number >> unit.shift; count > 0 && number%(1<<unit.shift) == 0 {
				return fmt.Sprint(count) + " " + unit.name
			}
		}
		return fmt.Sprint(number) + " bytes"
	}
	if !duration {
		return fmt.Sprint(number)
	}
	for _, unit := range []struct {
		size time.Duration
		name string
	}{{time.Hour, "hour"}, {time.Minute, "minute"}, {time.Second, "second"}} {
		if count := time.Duration(number) / unit.size; count > 0 && time.Duration(number)%unit.size == 0 {
			if count == 1 {
				return "1 " + unit.name
			}
			return fmt.Sprint(int64(count)) + " " + unit.name + "s"
		}
	}
	t.Fatalf("duration %v is not a whole number of seconds", time.Duration(number))
	return ""
}
