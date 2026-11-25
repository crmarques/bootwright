package architecture_test

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const apiPackage = "github.com/crmarques/bootwright/api/v1alpha1"

type fieldRead struct {
	where    string
	path     []string
	anchored bool
}

type shapeRoots struct {
	kinds []*api.Shape
	every []*api.Shape
}

// foreignValueReads are the literal paths production code reads from a value
// whose vocabulary is no Bootwright schema's: an open native document, a
// document envelope or a value the product builds itself. Each key is a read
// path and its reason names the vocabulary that owns it. The list only shrinks.
func foreignValueReads() map[string]string {
	nmstate := "NMState desired-state key, inside the open native network state"
	ceph := "Ceph option name, inside the open spec.config.osd map"
	return map[string]string{
		"interfaces": nmstate, "state": nmstate, "routes": nmstate, "routes.config": nmstate, "destination": nmstate,
		"next-hop-address": nmstate, "next-hop-interface": nmstate, "dhcp": nmstate, "autoconf": nmstate,
		"mac-address": nmstate, "ipv4.address": nmstate, "ipv6.address": nmstate, "link-aggregation": nmstate,
		"link-aggregation.port": nmstate, "options": nmstate, "vlan.id": nmstate, "vlan.base-iface": nmstate,
		"base-iface": nmstate, "bridge.port": nmstate, "dns-resolver.config.server": nmstate, "ovn": nmstate, "ovs-db": nmstate,
		"nameResolutionRefs":         "a key the native network state may no longer carry, read only to refuse it",
		"osd_memory_target":          ceph,
		"osd_memory_target_autotune": ceph,
		"Statement":                  "AWS IAM policy document, a native body",
		"Version":                    "AWS IAM policy document, a native body",
		"metadata":                   "the document envelope and a native resource's metadata",
		"metadata.name":              "the document envelope",
		"metadata.labels":            "the document envelope",
		"managed":                    "a field of the network segment value substrate.NetworkSegment builds, no schema's",
	}
}

// undeclaredFieldReads are the literal paths production code reads from an API
// value that no schema declares and no foreign vocabulary explains, each with
// what the read means. The list only shrinks: fix the read or the schema, then
// remove the entry.
func undeclaredFieldReads() map[string]string {
	return map[string]string{}
}

func TestEveryLiteralAPIFieldReadIsDeclaredBySomeSchema(t *testing.T) {
	reads := apiFieldReads(productionSources(t))
	for _, problem := range undeclaredReadProblems(reads, loadShapeRoots(), foreignValueReads(), undeclaredFieldReads()) {
		t.Error(problem)
	}
}

func undeclaredReadProblems(reads []fieldRead, roots shapeRoots, foreign, known map[string]string) []string {
	var problems []string
	seen := map[string]bool{}
	for _, read := range reads {
		key := read.key()
		if roots.resolves(read) {
			continue
		}
		seen[key] = true
		_, isForeign := foreign[key]
		_, isKnown := known[key]
		if !isForeign && !isKnown {
			problems = append(problems, read.where+" reads "+key+", which no API schema declares; fix the path or declare the field")
		}
	}
	for _, list := range []map[string]string{foreign, known} {
		for _, key := range sortedKeys(list) {
			if !seen[key] {
				problems = append(problems, "a read exception lists "+key+", which production code no longer reads undeclared; remove it")
			}
		}
	}
	return problems
}

func (r fieldRead) key() string {
	prefix := ""
	if r.anchored {
		prefix = "spec."
	}
	return prefix + strings.Join(r.path, ".")
}

func loadShapeRoots() shapeRoots {
	var roots shapeRoots
	for _, kind := range api.Kinds() {
		if shape := api.Schema(kind); shape != nil {
			roots.kinds = append(roots.kinds, shape)
			collectShapes(shape, &roots.every)
		}
	}
	for _, shape := range nativeSchemas() {
		collectShapes(shape, &roots.every)
	}
	return roots
}

func nativeSchemas() []*api.Shape {
	shapes := []*api.Shape{api.StorageNFSExportNativeSchema(), api.StorageSignOnNativeSchema()}
	for _, resource := range []string{api.SMBCluster, api.SMBShare, api.SMBJoinAuth, api.SMBUsersGroups} {
		shapes = append(shapes, api.StorageSMBResourceNativeSchema(resource))
	}
	for _, service := range []string{
		"mon", "mgr", "mds", "rbd-mirror", "osd", "rgw", "ingress", "nfs", "prometheus", "alertmanager", "grafana",
		"node-exporter", "loki", "alloy", "promtail", "ceph-exporter", "snmp-gateway", "mgmt-gateway", "nvmeof",
	} {
		shapes = append(shapes, api.StorageNativeServiceSpecSchema(service))
	}
	return shapes
}

func collectShapes(shape *api.Shape, into *[]*api.Shape) {
	if shape == nil || slices.Contains(*into, shape) {
		return
	}
	*into = append(*into, shape)
	for _, field := range shape.Fields {
		collectShapes(field.Shape, into)
	}
	collectShapes(shape.Element, into)
	for _, alternative := range shape.Alternatives {
		collectShapes(alternative, into)
	}
}

func (r shapeRoots) resolves(read fieldRead) bool {
	candidates := r.every
	if read.anchored {
		candidates = r.kinds
	}
	return slices.ContainsFunc(candidates, func(shape *api.Shape) bool { return !shape.Open && shapeDeclares(shape, read.path) })
}

func shapeDeclares(shape *api.Shape, path []string) bool {
	if len(path) == 0 {
		return true
	}
	if shape == nil {
		return false
	}
	if shape.Open {
		return true
	}
	if slices.ContainsFunc(shape.Alternatives, func(alternative *api.Shape) bool { return shapeDeclares(alternative, path) }) {
		return true
	}
	declared, found := shape.Field(path[0])
	return found && shapeDeclares(declared.Shape, path[1:])
}

func apiFieldReads(sources []sourceFile) []fieldRead {
	var reads []fieldRead
	for _, source := range sources {
		if !slices.ContainsFunc(source.imports, func(imported packageImport) bool { return imported.path == apiPackage }) &&
			source.owner != "api/v1alpha1" {
			continue
		}
		covered := map[*ast.CallExpr]bool{}
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || covered[call] {
				return true
			}
			if read, found := fieldReadOf(call, covered); found {
				read.where = source.path + ":" + strconv.Itoa(positionLine(source, call.Pos()))
				reads = append(reads, read)
			}
			return true
		})
	}
	return reads
}

func positionLine(source sourceFile, position token.Pos) int {
	if source.fileSet == nil {
		return 0
	}
	return source.fileSet.Position(position).Line
}

func fieldReadOf(call *ast.CallExpr, covered map[*ast.CallExpr]bool) (fieldRead, bool) {
	literals, whole := literalArguments(call)
	if !accessorCall(call) || len(literals) == 0 {
		return fieldRead{}, false
	}
	read := fieldRead{path: literals}
	if !whole {
		return read, true
	}
	receiver := call.Fun.(*ast.SelectorExpr).X
	for {
		inner, ok := receiver.(*ast.CallExpr)
		if !ok {
			return read, true
		}
		if spec, isSpec := inner.Fun.(*ast.SelectorExpr); isSpec && spec.Sel.Name == "Spec" && len(inner.Args) == 0 {
			read.anchored = true
			return read, true
		}
		prefix, prefixWhole := literalArguments(inner)
		if !accessorCall(inner) || !prefixWhole || len(prefix) == 0 {
			return read, true
		}
		covered[inner] = true
		read.path = append(slices.Clone(prefix), read.path...)
		receiver = inner.Fun.(*ast.SelectorExpr).X
	}
}

func accessorCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && (selector.Sel.Name == "Get" || selector.Sel.Name == "Has") && !call.Ellipsis.IsValid()
}

func literalArguments(call *ast.CallExpr) ([]string, bool) {
	var literals []string
	for _, argument := range call.Args {
		literal, ok := argument.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return literals, false
		}
		text, err := strconv.Unquote(literal.Value)
		if err != nil {
			return literals, false
		}
		literals = append(literals, text)
	}
	return literals, true
}

// TestAPIFieldReadsRefuseWhatTheyGuard plants declared and undeclared paths of
// each accessor form, a foreign read and a stale exception, and holds the
// check to reporting exactly the undeclared reads and the stale entry.
func TestAPIFieldReadsRefuseWhatTheyGuard(t *testing.T) {
	source := compositionSource(t, "internal/consumer", `package consumer
import api "`+apiPackage+`"
func read(object api.Object, value api.Value) {
	_ = object.Spec().Get("install", "nodeSSH", "keyPairRef")
	_ = object.Spec().Get("install").Get("nodeSSHTYPO")
	_ = value.Has("machineRef")
	_ = value.Get("noSuchFieldAnywhere").Has("deeper")
	_ = value.Get("machineRef", dynamic())
	_ = value.Get("foreignKey")
}
`)
	reads := apiFieldReads([]sourceFile{source})
	got := undeclaredReadProblems(reads, loadShapeRoots(), map[string]string{"foreignKey": "x", "goneKey": "y"}, nil)
	want := []string{
		"internal/consumer/fixture.go:0 reads noSuchFieldAnywhere.deeper, which no API schema declares; fix the path or declare the field",
		"internal/consumer/fixture.go:0 reads spec.install.nodeSSHTYPO, which no API schema declares; fix the path or declare the field",
		"a read exception lists goneKey, which production code no longer reads undeclared; remove it",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
