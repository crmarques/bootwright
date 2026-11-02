package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const lifecycleDirectory = "../../internal/reconciliation/lifecycle"

func lifecycleFileClasses() map[string]string {
	return map[string]string{
		"additive.go": "planner", "authorization.go": "planner", "claims.go": "planner", "commands.go": "planner",
		"contradictions.go": "planner", "decision.go": "planner", "lineage.go": "planner", "lineage_removal.go": "planner",
		"operation_plan.go": "planner", "other_build.go": "planner", "planning.go": "planner", "presentation.go": "planner",
		"removal_phases.go": "planner", "removal_plan.go": "planner", "secret_references.go": "planner",
		"stage_tails.go": "planner", "transitions.go": "planner",
		"attempts.go": "executor", "evidence.go": "executor", "executor.go": "executor", "frozenbinding.go": "executor",
		"invocation.go": "executor", "logging.go": "executor", "online_gates.go": "executor", "produced.go": "executor",
		"rebind.go": "executor", "register_run.go": "executor", "registration.go": "executor",
		"removal_evidence.go": "executor", "removal_gates.go": "executor", "runtime.go": "executor",
		"scheduler.go": "executor", "settle.go": "executor",
		"execution.go": "orchestrator", "finalization.go": "orchestrator",
		"diff.go": "reader", "diff_compare.go": "reader", "diff_observe.go": "reader", "handoff.go": "reader",
		"projection.go": "reader", "status.go": "reader", "status_readiness.go": "reader",
		"status_resources.go": "reader", "unresolved.go": "reader",
		"binding_proof.go": "shared", "contracts.go": "shared", "produced_inputs.go": "shared", "remedies.go": "shared",
		"reproof.go": "shared", "requests.go": "shared", "service.go": "shared", "support.go": "shared",
	}
}

func lifecyclePlannerEntries() []string {
	return []string{
		"Service.decideAs", "Service.decideFrom", "Service.decideOver", "Service.decideBy", "Service.freshApply",
		"Service.freshDestroy", "Service.supersede", "Service.planFrom", "Service.removalOf", "Service.additive",
		"Service.computePhases", "Service.presentation", "planPreview", "authorize",
	}
}

func lifecycleEffectSinks() []string {
	return []string{
		"Service.execute", "Service.protect", "Service.bind", "Service.run", "Service.register", "Service.resume",
		"Service.converge", "scheduler.converge", "Service.newScheduler", "Service.attempt", "Service.resolveUnknown",
		"Service.invoke", "Service.finish", "Service.finalize", "Service.finalizeFirst", "Service.releaseUnclaimed",
		"Service.completeRemoval", "Service.collect", "Service.proveRemovable", "Service.proveQuiescent",
	}
}

func lifecycleForbiddenCalls() []string {
	return []string{
		"MutateLifecycle", "RunLifecycle", "OpenRun", "Bind", "Release", "Produce", "Withdraw", "Reopen", "ReadCurrent",
		"ReadProduced", "ReadProducedInArea", "Rebind", "Rebuild", "WithPython",
		"ClaimAdding", "Reclaim", "Register", "UpdateOperation", "AdmitAttempt", "StartAttempt", "RecordPreparation",
		"CompleteAttempt", "StartResolution", "CompleteResolution", "OpenLog", "OpenAdapterOutput",
		"Apply", "Destroy", "Observe", "ObserveRemoval", "Quiescent", "Keep",
		"PublishEvidence", "Reserve", "Claim", "ReleaseReservations", "ReproveReservations", "ClientArea",
		"SealClientArea", "RetireToolAreas", "RetainDependencies", "Secrets",
	}
}

func lifecycleExecutorEntries() []string {
	return []string{
		"Service.execute", "Service.run", "Service.register", "Service.resume", "Service.converge", "scheduler.converge",
		"Service.attempt", "Service.resolveUnknown", "Service.finish", "Service.finalize",
	}
}

func lifecycleReplanningTargets() []string {
	return []string{
		"Service.planFrom", "Service.freshApply", "Service.freshDestroy", "Service.removalOf", "Service.computePhases",
		"Service.additive", "Service.decideFrom", "Service.decideOver", "Service.decideBy", "Service.decideAs",
	}
}

func lifecyclePlannerEdgeExemptions() []string {
	return []string{}
}

func lifecyclePlannerCallExemptions() []string {
	return []string{"Service.producedInContext:ReadProduced", "Service.rebuild:Rebuild"}
}

type lifecycleFunc struct {
	key, name, receiver, file string
	declaration               *ast.FuncDecl
	imports                   map[string]bool
	typeParams                map[string]bool
}

type lifecycleGraph struct {
	funcs   map[string]*lifecycleFunc
	order   []string
	edges   map[string][]string
	ports   map[string]map[string]bool
	handles map[string]bool
}

type lifecycleTypes struct {
	fields     map[string]map[string]string
	embedded   map[string][]string
	named      map[string]bool
	interfaces map[string]bool
	results    map[string][]string
}

type lifecycleScope struct {
	parent *lifecycleScope
	kinds  map[string]string
}

func newLifecycleScope(parent *lifecycleScope) *lifecycleScope {
	return &lifecycleScope{parent: parent, kinds: map[string]string{}}
}

func (scope *lifecycleScope) lookup(name string) (string, bool) {
	for current := scope; current != nil; current = current.parent {
		if kind, ok := current.kinds[name]; ok {
			return kind, true
		}
	}
	return "", false
}

func (scope *lifecycleScope) bind(name, kind string) {
	if name == "_" {
		return
	}
	if previous, seen := scope.kinds[name]; seen && previous != kind {
		kind = "?"
	}
	scope.kinds[name] = kind
}

func loadLifecycleGraph(t *testing.T) *lifecycleGraph {
	t.Helper()
	entries, err := os.ReadDir(lifecycleDirectory)
	if err != nil {
		t.Fatal(err)
	}
	types := lifecycleTypes{
		fields: map[string]map[string]string{}, embedded: map[string][]string{}, named: map[string]bool{},
		interfaces: map[string]bool{}, results: map[string][]string{},
	}
	graph := &lifecycleGraph{funcs: map[string]*lifecycleFunc{}, edges: map[string][]string{}, ports: map[string]map[string]bool{}, handles: map[string]bool{}}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		syntax, err := parser.ParseFile(token.NewFileSet(), filepath.Join(lifecycleDirectory, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		imports := map[string]bool{}
		for _, imported := range syntax.Imports {
			alias := strings.Trim(imported.Path.Value, `"`)
			alias = alias[strings.LastIndex(alias, "/")+1:]
			if imported.Name != nil {
				alias = imported.Name.Name
			}
			imports[alias] = true
		}
		for _, declaration := range syntax.Decls {
			switch declared := declaration.(type) {
			case *ast.GenDecl:
				types.collect(declared)
			case *ast.FuncDecl:
				graph.declare(declared, name, imports, types)
			}
		}
	}
	sort.Strings(graph.order)
	for _, key := range graph.order {
		graph.link(key, types)
	}
	return graph
}

func (graph *lifecycleGraph) declare(declared *ast.FuncDecl, file string, imports map[string]bool, types lifecycleTypes) {
	function := &lifecycleFunc{name: declared.Name.Name, file: file, declaration: declared, imports: imports, typeParams: lifecycleParamNames(declared.Type.TypeParams)}
	function.key = function.name
	switch {
	case declared.Recv != nil && len(declared.Recv.List) == 1:
		function.receiver = lifecycleTypeName(declared.Recv.List[0].Type)
		function.key = function.receiver + "." + function.name
		maps.Copy(function.typeParams, lifecycleReceiverParams(declared.Recv.List[0].Type))
	case function.name == "init":
		function.key = "init#" + file + "#" + strconv.Itoa(len(graph.order))
	}
	graph.funcs[function.key] = function
	graph.order = append(graph.order, function.key)
	types.results[function.key] = lifecycleResults(declared.Type.Results, function.typeParams)
}

func lifecycleParamNames(list *ast.FieldList) map[string]bool {
	names := map[string]bool{}
	if list != nil {
		for _, field := range list.List {
			for _, name := range field.Names {
				names[name.Name] = true
			}
		}
	}
	return names
}

func lifecycleReceiverParams(expression ast.Expr) map[string]bool {
	names := map[string]bool{}
	var indices []ast.Expr
	switch typed := lifecycleUnparen(expression).(type) {
	case *ast.IndexExpr:
		indices = []ast.Expr{typed.Index}
	case *ast.IndexListExpr:
		indices = typed.Indices
	}
	for _, index := range indices {
		if name, ok := index.(*ast.Ident); ok {
			names[name.Name] = true
		}
	}
	return names
}

func lifecycleUnparen(expression ast.Expr) ast.Expr {
	for {
		switch typed := expression.(type) {
		case *ast.StarExpr:
			expression = typed.X
		case *ast.ParenExpr:
			expression = typed.X
		default:
			return expression
		}
	}
}

func (types lifecycleTypes) collect(declared *ast.GenDecl) {
	for _, spec := range declared.Specs {
		typed, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		name := typed.Name.Name
		types.named[name] = true
		params := lifecycleParamNames(typed.TypeParams)
		switch body := typed.Type.(type) {
		case *ast.InterfaceType:
			types.interfaces[name] = true
			for _, method := range body.Methods.List {
				if signature, ok := method.Type.(*ast.FuncType); ok {
					for _, declared := range method.Names {
						types.results[name+"."+declared.Name] = lifecycleResults(signature.Results, params)
					}
				} else if len(method.Names) == 0 {
					types.embedded[name] = append(types.embedded[name], lifecycleTypeName(method.Type))
				}
			}
		case *ast.StructType:
			fields := map[string]string{}
			for _, field := range body.Fields.List {
				kind := lifecycleKind(field.Type, params)
				for _, declared := range field.Names {
					fields[declared.Name] = kind
				}
				if len(field.Names) == 0 {
					fields[kind[strings.LastIndex(kind, ".")+1:]] = kind
					types.embedded[name] = append(types.embedded[name], kind)
				}
			}
			types.fields[name] = fields
		}
	}
}

func lifecycleKind(expression ast.Expr, params map[string]bool) string {
	kind := lifecycleTypeName(expression)
	if params[kind] {
		return "?"
	}
	return kind
}

func lifecycleResults(list *ast.FieldList, params map[string]bool) []string {
	var kinds []string
	if list == nil {
		return kinds
	}
	for _, field := range list.List {
		for range max(len(field.Names), 1) {
			kinds = append(kinds, lifecycleKind(field.Type, params))
		}
	}
	return kinds
}

func lifecycleTypeName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return lifecycleTypeName(typed.X)
	case *ast.ParenExpr:
		return lifecycleTypeName(typed.X)
	case *ast.IndexExpr:
		return lifecycleTypeName(typed.X)
	case *ast.IndexListExpr:
		return lifecycleTypeName(typed.X)
	case *ast.SelectorExpr:
		return lifecycleTypeName(typed.X) + "." + typed.Sel.Name
	default:
		return "."
	}
}

// owner returns the declared type that supplies the method name for kind: kind
// itself, or an embedded type (recursively) that promotes it.
func (types lifecycleTypes) owner(kind, name string) (string, bool) {
	return types.ownerSeen(kind, name, map[string]bool{})
}

func (types lifecycleTypes) ownerSeen(kind, name string, seen map[string]bool) (string, bool) {
	if _, ok := types.results[kind+"."+name]; ok {
		return kind, true
	}
	if seen[kind] {
		return "", false
	}
	seen[kind] = true
	for _, embedded := range types.embedded[kind] {
		if owner, ok := types.ownerSeen(embedded, name, seen); ok {
			return owner, true
		}
	}
	return "", false
}

func (types lifecycleTypes) interfaceMethods(contract string, seen map[string]bool) []string {
	if seen[contract] {
		return nil
	}
	seen[contract] = true
	var methods []string
	for key := range types.results {
		if method, ok := strings.CutPrefix(key, contract+"."); ok && !strings.Contains(method, ".") {
			methods = append(methods, method)
		}
	}
	for _, embedded := range types.embedded[contract] {
		methods = append(methods, types.interfaceMethods(embedded, seen)...)
	}
	return methods
}

func (types lifecycleTypes) fieldType(kind, name string, seen map[string]bool) (string, bool) {
	if fields, ok := types.fields[kind]; ok {
		if found, ok := fields[name]; ok {
			return found, true
		}
	}
	if seen[kind] {
		return "", false
	}
	seen[kind] = true
	for _, embedded := range types.embedded[kind] {
		if found, ok := types.fieldType(embedded, name, seen); ok {
			return found, true
		}
	}
	return "", false
}

func (types lifecycleTypes) holdsTransaction(kind string, seen map[string]bool) bool {
	if kind == "Transaction" {
		return true
	}
	if seen[kind] {
		return false
	}
	seen[kind] = true
	for _, field := range types.fields[kind] {
		if types.holdsTransaction(field, seen) {
			return true
		}
	}
	return slices.ContainsFunc(types.embedded[kind], func(embedded string) bool { return types.holdsTransaction(embedded, seen) })
}

func (types lifecycleTypes) assign(scope *lifecycleScope, left, right []ast.Expr) {
	var kinds []string
	switch {
	case len(left) == len(right):
		for _, value := range right {
			kind, ok := types.valueType(value, scope)
			if !ok {
				kind = "?"
			}
			kinds = append(kinds, kind)
		}
	case len(right) == 1:
		if call, ok := right[0].(*ast.CallExpr); ok {
			kinds = types.callResults(call, scope)
		}
	}
	for index, side := range left {
		name, ok := side.(*ast.Ident)
		if !ok {
			continue
		}
		kind := "?"
		if index < len(kinds) {
			kind = kinds[index]
		}
		scope.bind(name.Name, kind)
	}
}

func (types lifecycleTypes) callResults(call *ast.CallExpr, scope *lifecycleScope) []string {
	switch callee := call.Fun.(type) {
	case *ast.Ident:
		if _, bound := scope.lookup(callee.Name); bound {
			return nil
		}
		if types.named[callee.Name] {
			return []string{callee.Name}
		}
		return types.results[callee.Name]
	case *ast.SelectorExpr:
		if kind, ok := types.typeOf(callee.X, scope); ok {
			if owner, found := types.owner(kind, callee.Sel.Name); found {
				return types.results[owner+"."+callee.Sel.Name]
			}
		}
	}
	return nil
}

func (types lifecycleTypes) valueType(value ast.Expr, scope *lifecycleScope) (string, bool) {
	switch typed := value.(type) {
	case *ast.CompositeLit:
		if typed.Type == nil {
			return "", false
		}
		return lifecycleTypeName(typed.Type), true
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			return types.valueType(typed.X, scope)
		}
	case *ast.TypeAssertExpr:
		if typed.Type != nil {
			return lifecycleTypeName(typed.Type), true
		}
	case *ast.CallExpr:
		if kinds := types.callResults(typed, scope); len(kinds) == 1 {
			return kinds[0], kinds[0] != "?"
		}
	case *ast.BasicLit:
		return ".", true
	default:
		return types.typeOf(value, scope)
	}
	return "", false
}

func (types lifecycleTypes) typeOf(expression ast.Expr, scope *lifecycleScope) (string, bool) {
	switch typed := expression.(type) {
	case *ast.Ident:
		kind, ok := scope.lookup(typed.Name)
		return kind, ok && kind != "?"
	case *ast.ParenExpr:
		return types.typeOf(typed.X, scope)
	case *ast.StarExpr:
		return types.typeOf(typed.X, scope)
	case *ast.SelectorExpr:
		owner, ok := types.typeOf(typed.X, scope)
		if !ok {
			return "", false
		}
		if _, declared := types.fields[owner]; !declared {
			return ".", true
		}
		kind, ok := types.fieldType(owner, typed.Sel.Name, map[string]bool{})
		return kind, ok && kind != "?"
	case *ast.CallExpr:
		if kinds := types.callResults(typed, scope); len(kinds) == 1 && kinds[0] != "?" {
			return kinds[0], true
		}
	}
	return "", false
}

func (graph *lifecycleGraph) checkHandles(key string, list *ast.FieldList, types lifecycleTypes) {
	if list == nil {
		return
	}
	for _, field := range list.List {
		if types.holdsTransaction(lifecycleTypeName(field.Type), map[string]bool{}) {
			graph.handles[key] = true
		}
	}
}

func (graph *lifecycleGraph) every(name string) []string {
	var every []string
	for _, key := range graph.order {
		if function := graph.funcs[key]; function.receiver != "" && function.name == name {
			every = append(every, key)
		}
	}
	return every
}

func (graph *lifecycleGraph) method(receiver, name string) []string {
	if _, ok := graph.funcs[receiver+"."+name]; ok {
		return []string{receiver + "." + name}
	}
	return nil
}

type lifecycleLinker struct {
	graph    *lifecycleGraph
	types    lifecycleTypes
	key      string
	function *lifecycleFunc
	callees  map[string]bool
}

func (graph *lifecycleGraph) link(key string, types lifecycleTypes) {
	function := graph.funcs[key]
	if function.declaration.Body == nil {
		return
	}
	linker := &lifecycleLinker{graph: graph, types: types, key: key, function: function, callees: map[string]bool{}}
	scope := newLifecycleScope(nil)
	graph.checkHandles(key, function.declaration.Recv, types)
	graph.checkHandles(key, function.declaration.Type.Params, types)
	for _, list := range []*ast.FieldList{function.declaration.Recv, function.declaration.Type.Params, function.declaration.Type.Results} {
		linker.declare(scope, list)
	}
	graph.ports[key] = map[string]bool{}
	linker.walk(function.declaration.Body, scope)
	for callee := range linker.callees {
		graph.edges[key] = append(graph.edges[key], callee)
	}
	sort.Strings(graph.edges[key])
}

func (linker *lifecycleLinker) declare(scope *lifecycleScope, list *ast.FieldList) {
	if list == nil {
		return
	}
	for _, field := range list.List {
		for _, name := range field.Names {
			scope.bind(name.Name, lifecycleKind(field.Type, linker.function.typeParams))
		}
	}
}

// resolve returns the declared functions a selector may call, and whether the
// call goes through a port: a receiver of unknown type or one of a
// package-declared interface (directly or promoted by an embedded interface).
// A receiver whose declared struct type is evident resolves to that type's
// method alone, found through embedded fields; an interface or unknown
// receiver over-approximates to every declared method with that name.
func (linker *lifecycleLinker) resolve(selector *ast.SelectorExpr, scope *lifecycleScope) ([]string, bool) {
	name := selector.Sel.Name
	types, graph := linker.types, linker.graph
	if identifier, ok := selector.X.(*ast.Ident); ok {
		if _, bound := scope.lookup(identifier.Name); !bound {
			if linker.function.imports[identifier.Name] {
				return nil, false
			}
			if types.named[identifier.Name] {
				if types.interfaces[identifier.Name] {
					return linker.implementing(identifier.Name, name), true
				}
				return linker.promoted(identifier.Name, name)
			}
		}
	}
	kind, known := types.typeOf(selector.X, scope)
	switch {
	case !known:
		return graph.every(name), true
	case types.interfaces[kind]:
		return linker.implementing(kind, name), true
	}
	return linker.promoted(kind, name)
}

func (linker *lifecycleLinker) promoted(kind, name string) ([]string, bool) {
	owner, found := linker.types.owner(kind, name)
	switch {
	case !found:
		return nil, false
	case linker.types.interfaces[owner]:
		return linker.implementing(owner, name), true
	}
	return linker.graph.method(owner, name), false
}

func (linker *lifecycleLinker) implementing(contract, name string) []string {
	methods := linker.types.interfaceMethods(contract, map[string]bool{})
	var keys []string
	for _, key := range linker.graph.every(name) {
		implements := true
		for _, method := range methods {
			owner, found := linker.types.owner(linker.graph.funcs[key].receiver, method)
			implements = implements && found && !linker.types.interfaces[owner]
		}
		if implements {
			keys = append(keys, key)
		}
	}
	return keys
}

func (linker *lifecycleLinker) walkAll(scope *lifecycleScope, nodes ...ast.Node) {
	for _, node := range nodes {
		if node != nil && !reflect.ValueOf(node).IsNil() {
			linker.walk(node, scope)
		}
	}
}

func (linker *lifecycleLinker) walk(node ast.Node, scope *lifecycleScope) {
	ast.Inspect(node, func(visited ast.Node) bool {
		switch typed := visited.(type) {
		case *ast.FuncLit:
			inner := newLifecycleScope(scope)
			linker.graph.checkHandles(linker.key, typed.Type.Params, linker.types)
			linker.declare(inner, typed.Type.Params)
			linker.declare(inner, typed.Type.Results)
			linker.walk(typed.Body, inner)
		case *ast.BlockStmt:
			inner := newLifecycleScope(scope)
			for _, statement := range typed.List {
				linker.walk(statement, inner)
			}
		case *ast.IfStmt:
			linker.walkAll(newLifecycleScope(scope), typed.Init, typed.Cond, typed.Body, typed.Else)
		case *ast.ForStmt:
			linker.walkAll(newLifecycleScope(scope), typed.Init, typed.Cond, typed.Post, typed.Body)
		case *ast.SwitchStmt:
			linker.walkAll(newLifecycleScope(scope), typed.Init, typed.Tag, typed.Body)
		case *ast.TypeSwitchStmt:
			linker.walkAll(newLifecycleScope(scope), typed.Init, typed.Assign, typed.Body)
		case *ast.RangeStmt:
			inner := newLifecycleScope(scope)
			linker.walk(typed.X, inner)
			for _, side := range []ast.Expr{typed.Key, typed.Value} {
				if name, ok := side.(*ast.Ident); ok && typed.Tok == token.DEFINE {
					inner.bind(name.Name, "?")
				} else if side != nil && typed.Tok != token.DEFINE {
					linker.walk(side, inner)
				}
			}
			linker.walk(typed.Body, inner)
		case *ast.CaseClause:
			inner := newLifecycleScope(scope)
			for _, expression := range typed.List {
				linker.walk(expression, inner)
			}
			for _, statement := range typed.Body {
				linker.walk(statement, inner)
			}
		case *ast.CommClause:
			inner := newLifecycleScope(scope)
			linker.walkAll(inner, typed.Comm)
			for _, statement := range typed.Body {
				linker.walk(statement, inner)
			}
		default:
			return linker.visit(visited, scope)
		}
		return false
	})
}

func (linker *lifecycleLinker) visit(visited ast.Node, scope *lifecycleScope) bool {
	switch typed := visited.(type) {
	case *ast.AssignStmt:
		if typed.Tok == token.DEFINE {
			for _, value := range typed.Rhs {
				linker.walk(value, scope)
			}
			linker.types.assign(scope, typed.Lhs, typed.Rhs)
			return false
		}
	case *ast.ValueSpec:
		for _, value := range typed.Values {
			linker.walk(value, scope)
		}
		for index, name := range typed.Names {
			switch {
			case typed.Type != nil:
				scope.bind(name.Name, lifecycleKind(typed.Type, linker.function.typeParams))
			case len(typed.Values) == len(typed.Names):
				linker.types.assign(scope, []ast.Expr{name}, []ast.Expr{typed.Values[index]})
			default:
				scope.bind(name.Name, "?")
			}
		}
		return false
	case *ast.CompositeLit:
		for _, element := range typed.Elts {
			if pair, ok := element.(*ast.KeyValueExpr); ok && linker.structKeyed(typed) {
				if _, bare := pair.Key.(*ast.Ident); bare {
					linker.walk(pair.Value, scope)
					continue
				}
			}
			linker.walk(element, scope)
		}
		return false
	case *ast.Ident:
		if bare, ok := linker.graph.funcs[typed.Name]; ok && bare.receiver == "" {
			if _, shadowed := scope.lookup(typed.Name); !shadowed {
				linker.callees[bare.key] = true
			}
		}
	case *ast.SelectorExpr:
		callees, port := linker.resolve(typed, scope)
		if port {
			linker.graph.ports[linker.key][typed.Sel.Name] = true
		}
		for _, callee := range callees {
			linker.callees[callee] = true
		}
		linker.walk(typed.X, scope)
		return false
	}
	return true
}

func (linker *lifecycleLinker) structKeyed(literal *ast.CompositeLit) bool {
	switch literal.Type.(type) {
	case nil, *ast.StructType:
		return true
	case *ast.MapType, *ast.ArrayType:
		return false
	}
	name := lifecycleTypeName(literal.Type)
	if linker.types.named[name] {
		_, structure := linker.types.fields[name]
		return structure
	}
	return true
}

func (graph *lifecycleGraph) match(spec string) []string {
	if _, ok := graph.funcs[spec]; ok {
		return []string{spec}
	}
	return nil
}

func (graph *lifecycleGraph) matchAll(t *testing.T, kind string, specs []string) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, spec := range specs {
		found := graph.match(spec)
		if len(found) == 0 {
			t.Errorf("%s %q no longer names a lifecycle function; update the list in lifecycle_partition_test.go", kind, spec)
		}
		for _, key := range found {
			keys[key] = true
		}
	}
	return keys
}

type lifecycleWalk struct {
	parent map[string]string
	edges  map[string]bool
}

func (graph *lifecycleGraph) reach(entries map[string]bool, skip []string) lifecycleWalk {
	walk := lifecycleWalk{parent: map[string]string{}, edges: map[string]bool{}}
	var queue []string
	for key := range entries {
		walk.parent[key] = ""
		queue = append(queue, key)
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		for _, callee := range graph.edges[key] {
			edge := key + ">" + callee
			if slices.Contains(skip, edge) {
				walk.edges[edge] = true
				continue
			}
			if _, seen := walk.parent[callee]; !seen {
				walk.parent[callee] = key
				queue = append(queue, callee)
			}
		}
	}
	return walk
}

func (walk lifecycleWalk) path(key string) string {
	var chain []string
	for current := key; current != ""; current = walk.parent[current] {
		chain = append(chain, current)
	}
	slices.Reverse(chain)
	return strings.Join(chain, " -> ")
}

func (walk lifecycleWalk) reached() []string {
	keys := make([]string, 0, len(walk.parent))
	for key := range walk.parent {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestLifecyclePlannerReachesNoEffect(t *testing.T) {
	graph := loadLifecycleGraph(t)
	entries := graph.matchAll(t, "planner entry point", lifecyclePlannerEntries())
	sinks := graph.matchAll(t, "effect sink", lifecycleEffectSinks())
	walk := graph.reach(entries, lifecyclePlannerEdgeExemptions())
	for _, edge := range lifecyclePlannerEdgeExemptions() {
		if !walk.edges[edge] {
			t.Errorf("planner edge exemption %q no longer occurs; remove it", edge)
		}
	}
	usedCalls := map[string]bool{}
	for _, key := range walk.reached() {
		if sinks[key] {
			t.Errorf("planner reaches effect %s: %s", key, walk.path(key))
		}
		if graph.handles[key] {
			t.Errorf("planner reaches %s, which takes a Transaction: %s", key, walk.path(key))
		}
		for _, name := range lifecycleForbiddenCalls() {
			if !graph.ports[key][name] {
				continue
			}
			exemption := key + ":" + name
			if slices.Contains(lifecyclePlannerCallExemptions(), exemption) {
				usedCalls[exemption] = true
				continue
			}
			t.Errorf("planner reaches %s, which calls %s: %s", key, name, walk.path(key))
		}
	}
	for _, exemption := range lifecyclePlannerCallExemptions() {
		if !usedCalls[exemption] {
			t.Errorf("planner call exemption %q no longer occurs; remove it", exemption)
		}
	}
}

func TestLifecycleExecutorNeverReplans(t *testing.T) {
	graph := loadLifecycleGraph(t)
	entries := graph.matchAll(t, "executor entry point", lifecycleExecutorEntries())
	targets := graph.matchAll(t, "replanning target", lifecycleReplanningTargets())
	walk := graph.reach(entries, nil)
	for _, key := range walk.reached() {
		if targets[key] {
			t.Errorf("executor re-plans through %s: %s", key, walk.path(key))
		}
	}
}

func TestLifecycleFilesAreClassified(t *testing.T) {
	classes := lifecycleFileClasses()
	entries, err := os.ReadDir(lifecycleDirectory)
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		present[name] = true
		class, listed := classes[name]
		switch {
		case !listed:
			t.Errorf("%s has no class; add it to lifecycleFileClasses as planner, executor, orchestrator, reader or shared", name)
		case !slices.Contains([]string{"planner", "executor", "orchestrator", "reader", "shared"}, class):
			t.Errorf("%s has unknown class %q", name, class)
		}
	}
	for name := range classes {
		if !present[name] {
			t.Errorf("lifecycleFileClasses lists %s, which no longer exists; remove it", name)
		}
	}
}
