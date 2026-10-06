package compilation

import (
	"errors"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

var decimalInteger = regexp.MustCompile(`^[+-]?(?:0|[1-9][0-9]*)$`)
var leadingZeroInteger = regexp.MustCompile(`^[+-]?0[0-9]+$`)
var decimalNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
var schemaIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

const unsupportedSpellingRemedy = "write lowercase true or false, a decimal number without a base prefix or separator, or quote the value as a string"

type decoder struct {
	document  desiredstate.Document
	sink      *diagnosticSink
	locations map[string]diagnostics.SourceLocation
	object    *diagnostics.ObjectIdentity
	failed    bool
}

func decodeDocument(document desiredstate.Document, sink *diagnosticSink) *objectRecord {
	if document.IsEmpty() {
		return nil
	}
	d := decoder{document: document, sink: sink, locations: map[string]diagnostics.SourceLocation{}}
	root := documentBody(document)
	if root == nil || root.Kind != desiredstate.MappingKind {
		d.fail(root, "yaml.shape", "$", "a desired-state document must be a mapping", "write the document as a mapping of apiVersion, kind, metadata and spec")
		return nil
	}
	d.object = documentObject(root)
	kindNode := mappingNode(root, "kind")
	if kindNode == nil {
		d.fail(root, "api.kind", "$.kind", "kind must name a registered API kind", "add kind with a registered kind name")
		return nil
	}
	kindValue := d.value(kindNode, &api.Shape{Type: api.String}, "$.kind")
	if d.failed {
		return nil
	}
	kind := api.Kind(kindValue.Text())
	shape := api.Schema(kind)
	if shape == nil {
		message, remediation := unregisteredKind(kind)
		d.fail(mappingNode(root, "kind"), "api.kind", "$.kind", message, remediation)
		return nil
	}
	envelope := &api.Shape{Type: api.Mapping, Fields: []api.Field{
		{Name: "apiVersion", Shape: &api.Shape{Type: api.String}},
		{Name: "kind", Shape: &api.Shape{Type: api.String}},
		{Name: "metadata", Shape: &api.Shape{Type: api.Mapping, Fields: []api.Field{{Name: "name", Shape: &api.Shape{Type: api.String}}, {Name: "labels", Shape: &api.Shape{Type: api.Mapping, Open: true, Element: &api.Shape{Type: api.String}}}}}},
		{Name: "spec", Shape: shape},
	}}
	value := d.value(root, envelope, "$")
	if mappingNode(root, "apiVersion") == nil || value.Has("apiVersion") && value.Get("apiVersion").Text() != api.APIVersion {
		d.fail(mappingNode(root, "apiVersion"), "api.version", "$.apiVersion", "apiVersion must be bootwright.io/v1alpha1", "set apiVersion: bootwright.io/v1alpha1")
	}
	for _, key := range []string{"metadata", "spec"} {
		if mappingNode(root, key) == nil {
			d.fail(root, "api.required", "$."+key, "required envelope field is absent", "add "+key)
		}
	}
	if value.Has("metadata") && mappingNode(mappingNode(root, "metadata"), "name") == nil {
		d.fail(mappingNode(root, "metadata"), "api.required", "$.metadata.name", "metadata.name is required", "set metadata.name to a unique DNS label")
	}
	if d.failed {
		return nil
	}
	o := api.NewObject(kind, value.Get("metadata", "name").Text(), value.Get("metadata", "labels"), value.Get("spec"))
	return &objectRecord{object: o, authored: o, path: document.Path, document: document.Index, locations: d.locations, inherited: map[string]diagnostics.SourceLocation{}}
}

func documentBody(doc desiredstate.Document) *desiredstate.Node {
	if doc.Root == nil || len(doc.Root.Content) != 1 {
		return nil
	}
	return doc.Root.Content[0]
}

func mappingNode(node *desiredstate.Node, key string) *desiredstate.Node {
	if node == nil || node.Kind != desiredstate.MappingKind {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func nodeText(node *desiredstate.Node) string {
	if node == nil || node.Kind != desiredstate.ScalarKind {
		return ""
	}
	return node.Value
}

func documentObject(root *desiredstate.Node) *diagnostics.ObjectIdentity {
	apiVersion := stringScalar(mappingNode(root, "apiVersion"))
	kind := stringScalar(mappingNode(root, "kind"))
	name := stringScalar(mappingNode(mappingNode(root, "metadata"), "name"))
	if apiVersion != api.APIVersion || api.KindIndex(api.Kind(kind)) < 0 || !api.ValidLexical("name", name) {
		return nil
	}
	return &diagnostics.ObjectIdentity{APIVersion: apiVersion, Kind: kind, Name: name}
}

func stringScalar(node *desiredstate.Node) string {
	if scalarType(node) != api.String {
		return ""
	}
	return nodeText(node)
}

func unregisteredKind(kind api.Kind) (string, string) {
	switch kind {
	case "InfraComponent":
		return "InfraComponent is retired; declare Proxy, DNSServer, NTPServer, ArtifactServer, Registry, or LoadBalancer with management on its spec", "replace it with the typed service object it described"
	case "Context":
		return "a Context is a standalone context configuration document, not desired state", "keep it outside the input directory and pass it to context init or context update with -f"
	}
	if schemaIdentifier.MatchString(string(kind)) {
		if nearest, ok := nearestName(string(kind), kindNames()); ok {
			return "kind must name a registered API kind", "use " + nearest
		}
	}
	return "kind must name a registered API kind", "use a registered kind name"
}

func kindNames() []string {
	names := []string{}
	for _, kind := range api.Kinds() {
		names = append(names, string(kind))
	}
	return names
}

func (d *decoder) fail(node *desiredstate.Node, code, field, message, remediation string) {
	d.failed = true
	location := diagnostics.SourceLocation{Path: d.document.Path, Document: d.document.Index}
	if node != nil {
		location.Line = node.Line
		location.Column = node.Column
	}
	d.sink.add(diagnostics.Diagnostic{Severity: "error", Code: code, Message: message, Source: &location, Object: d.object, Field: field, Remediation: remediation})
}

func (d *decoder) value(node *desiredstate.Node, shape *api.Shape, path string) api.Value {
	if node == nil || d.sink.stopped() {
		return api.Value{}
	}
	if _, recorded := d.locations[path]; !recorded {
		d.locations[path] = diagnostics.SourceLocation{Path: d.document.Path, Document: d.document.Index, Line: node.Line, Column: node.Column}
	}
	if d.refusedConstruct(node, path) {
		return api.Value{}
	}
	if shape == nil {
		shape = &api.Shape{}
	}
	if len(shape.Alternatives) > 0 {
		matched := false
		for _, alternative := range shape.Alternatives {
			if acceptsNode(alternative, node) {
				shape = alternative
				matched = true
				break
			}
		}
		if !matched && node.Kind == desiredstate.ScalarKind && (nullNode(node) || scalarType(node) == api.Absent) {
			return d.scalar(node, &api.Shape{}, path)
		}
		if !matched {
			d.typeMismatch(node, shape, path)
			return api.Value{}
		}
	}
	switch node.Kind {
	case desiredstate.MappingKind:
		return d.mapping(node, shape, path)
	case desiredstate.SequenceKind:
		return d.sequence(node, shape, path)
	case desiredstate.ScalarKind:
		return d.scalar(node, shape, path)
	}
	d.fail(node, "yaml.shape", path, "unsupported YAML representation", "write a mapping, a list or a scalar here")
	return api.Value{}
}

func nullNode(node *desiredstate.Node) bool {
	return node.Tag == "!!null" && !(node.ExplicitTag && node.Tag == "!!str")
}

func (d *decoder) typeMismatch(node *desiredstate.Node, expected *api.Shape, path string) {
	found := typeName(scalarType(node))
	switch node.Kind {
	case desiredstate.MappingKind:
		found = typeName(api.Mapping)
	case desiredstate.SequenceKind:
		found = typeName(api.Sequence)
	}
	want := shapeTypeName(expected)
	message := "expected " + want + ", found " + found
	remediation := "write " + withArticle(want) + " here"
	accepted := []*api.Shape{expected}
	if len(expected.Alternatives) > 0 {
		accepted = expected.Alternatives
	}
	quoted := node.Kind == desiredstate.ScalarKind && node.Style != desiredstate.PlainStyle
	for _, shape := range accepted {
		if quoted && decodesAs(node.Value, shape.Type) {
			d.fail(node, "api.type", path, message+"; a quoted value is a string", "remove the quotes")
			return
		}
	}
	if scalar := scalarType(node); !quoted && (scalar == api.Boolean || scalar == api.Integer || scalar == api.Number) && slices.ContainsFunc(accepted, func(shape *api.Shape) bool { return shape.Type == api.String }) {
		remediation = "quote the value"
	}
	d.fail(node, "api.type", path, message, remediation)
}

func decodesAs(text string, t api.ValueType) bool {
	switch t {
	case api.Boolean:
		return text == "true" || text == "false"
	case api.Integer:
		return decimalInteger.MatchString(text)
	case api.Number:
		return !leadingZeroInteger.MatchString(text) && (decimalInteger.MatchString(text) || decimalNumber.MatchString(text))
	}
	return false
}

func (d *decoder) mapping(node *desiredstate.Node, shape *api.Shape, path string) api.Value {
	if shape.Type != api.Absent && shape.Type != api.Mapping {
		d.typeMismatch(node, shape, path)
		return api.Value{}
	}
	fields := []api.FieldValue{}
	seen := map[string]bool{}
	for i := 0; i+1 < len(node.Content) && !d.sink.stopped(); i += 2 {
		key, n := node.Content[i], node.Content[i+1]
		if key.Kind == desiredstate.ScalarKind && key.Value == "<<" && (key.Tag == "!!merge" || scalarType(key) == api.String) {
			d.fail(key, "yaml.alias", path, "merge keys are not permitted", "write the merged fields out in full")
			continue
		}
		if d.refusedConstruct(key, path) {
			continue
		}
		if key.Kind != desiredstate.ScalarKind || scalarType(key) != api.String {
			d.fail(key, "yaml.shape", path, "mapping keys must be strings", "write the key as a plain field name")
			continue
		}
		if seen[key.Value] {
			d.fail(key, "yaml.duplicate-key", path, "mapping keys must be unique", "remove the repeated key")
			continue
		}
		seen[key.Value] = true
		childPath := path
		// Native keys are unbounded authored data, not diagnostic field names.
		// Repeating a long key in every descendant's provenance would amplify
		// a byte-bounded source into an unbounded number of large path strings.
		if !shape.Open && shape.Type != api.Absent {
			childPath += "." + key.Value
		}
		var childShape *api.Shape
		if shape.KindDefaults {
			childShape = api.Schema(api.Kind(key.Value))
			if childShape == nil {
				d.unknownKindDefault(key, path)
				continue
			}
			if api.Kind(key.Value) == api.Environment {
				copy := *childShape
				copy.Fields = nil
				for _, f := range childShape.Fields {
					if f.Name != "defaults" {
						copy.Fields = append(copy.Fields, f)
					}
				}
				childShape = &copy
			}
		} else if shape.Type == api.Absent || shape.Open {
			childShape = shape.Element
		} else if field, ok := shape.Field(key.Value); ok {
			childShape = field.Shape
		} else {
			d.unknownField(key, shape, path)
			continue
		}
		fields = append(fields, api.FieldValue{Name: key.Value, Value: d.value(n, childShape, childPath)})
	}
	return api.MapValue(fields...)
}

func (d *decoder) unknownField(key *desiredstate.Node, shape *api.Shape, path string) {
	if !schemaIdentifier.MatchString(key.Value) {
		d.fail(key, "api.field", path, "a field name that is not a schema identifier is not permitted here", "remove the field")
		return
	}
	childPath := path + "." + key.Value
	if message := d.retiredFieldMessage(path, key.Value); message != "" {
		d.fail(key, "api.field", childPath, message, "remove the retired field")
		return
	}
	names := make([]string, 0, len(shape.Fields))
	for _, field := range shape.Fields {
		names = append(names, field.Name)
	}
	if len(names) == 0 {
		d.fail(key, "api.field", childPath, "unknown field; "+fieldLabel(path)+" permits no fields", "remove the field")
		return
	}
	remediation := "remove it or use a permitted field"
	if nearest, ok := nearestName(key.Value, names); ok {
		remediation = "rename it to " + nearest
	}
	d.fail(key, "api.field", childPath, "unknown field; "+fieldLabel(path)+" permits "+boundedList(names, ", "), remediation)
}

func (d *decoder) unknownKindDefault(key *desiredstate.Node, path string) {
	if !schemaIdentifier.MatchString(key.Value) {
		d.fail(key, "api.field", path, "kind defaults name no registered kind", "use a registered kind name such as Machine")
		return
	}
	remediation := "use a registered kind name such as Machine"
	if nearest, ok := nearestName(key.Value, kindNames()); ok {
		remediation = "rename it to " + nearest
	}
	d.fail(key, "api.field", path+"."+key.Value, "kind defaults name no registered kind", remediation)
}

func fieldLabel(path string) string {
	if path == "$" {
		return "the document"
	}
	return strings.TrimPrefix(path, "$.")
}

func (d *decoder) sequence(node *desiredstate.Node, shape *api.Shape, path string) api.Value {
	if shape.Type != api.Absent && shape.Type != api.Sequence {
		d.typeMismatch(node, shape, path)
		return api.Value{}
	}
	items := make([]api.Value, 0, len(node.Content))
	for i, n := range node.Content {
		if d.sink.stopped() {
			break
		}
		childPath := path
		if shape.Type != api.Absent {
			childPath += "[" + strconv.Itoa(i) + "]"
		}
		items = append(items, d.value(n, shape.Element, childPath))
	}
	return api.ListValue(items...)
}

func (d *decoder) scalar(node *desiredstate.Node, shape *api.Shape, path string) api.Value {
	kind := scalarType(node)
	if nullNode(node) {
		d.fail(node, "api.type", path, "null values are not permitted", "omit the field instead of writing null")
		return api.Value{}
	}
	if leadingZero(node) {
		d.fail(node, "api.type", path, "a multi-digit integer must not start with 0; YAML 1.1 readers read it as octal", "remove the leading zero, or quote the value where the field takes a string")
		return api.Value{}
	}
	if kind == api.Absent {
		d.fail(node, "api.type", path, "scalar spelling or YAML type is not permitted", unsupportedSpellingRemedy)
		return api.Value{}
	}
	if shape.Type != api.Absent && kind != shape.Type && !(shape.Type == api.Number && kind == api.Integer) {
		d.typeMismatch(node, shape, path)
		return api.Value{}
	}
	switch kind {
	case api.String:
		return api.StringValue(node.Value)
	case api.Boolean:
		return api.BoolValue(node.Value == "true")
	case api.Integer:
		if shape.Type != api.Number {
			return api.IntegerValue(node.Value)
		}
		fallthrough
	case api.Number:
		n, err := strconv.ParseFloat(node.Value, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) || math.IsInf(n, 0) || math.IsNaN(n) {
			d.fail(node, "api.type", path, "number must be finite", "write a finite decimal number")
			return api.Value{}
		}
		return api.NumberValue(strconv.FormatFloat(n, 'g', -1, 64))
	}
	d.fail(node, "yaml.shape", path, "unsupported YAML representation", "write a mapping, a list or a scalar here")
	return api.Value{}
}

func (d *decoder) refusedConstruct(node *desiredstate.Node, path string) bool {
	if node.Kind == desiredstate.AliasKind || node.Anchor != "" {
		d.fail(node, "yaml.alias", path, "anchors and aliases are not permitted", "write the value out in full instead of an anchor or alias")
		return true
	}
	if node.ExplicitTag && !allowedTag(node) {
		d.fail(node, "yaml.tag", path, "the YAML tag is not permitted", "remove the tag")
		return true
	}
	return false
}

func (d *decoder) retiredFieldMessage(path, field string) string {
	kind := nodeText(mappingNode(documentBody(d.document), "kind"))
	if field == "proxy" {
		if kind == string(api.Environment) && (path == "$.spec.controller" || path == "$.spec.defaults.Environment.controller") {
			return "Environment controller.proxy is retired; put proxy on the Machine selected by controller.machineRef"
		}
		if kind == string(api.Machine) && path == "$.spec.os.install" || kind == string(api.Environment) && path == "$.spec.defaults.Machine.os.install" {
			return "Machine os.install.proxy is retired; use Machine.spec.proxy or its kind defaults"
		}
	}
	if kind == string(api.Environment) && (path == "$.spec" || path == "$.spec.defaults.Environment") {
		switch field {
		case "infraComponents":
			return "infraComponents is retired; declare typed service objects and reference them from their consumers"
		case "proxy":
			return "Environment proxy is retired; use Machine.spec.proxy on the controller and proxy choices on install consumers or their kind defaults"
		case "registries":
			return "Environment registries is retired; declare Registry objects and select them through ContainerCluster install.registries"
		case "componentImages":
			return "componentImages is retired; put image pins on managed service objects or their kind defaults"
		case "trustedCAs":
			return "Environment trustedCAs is retired; reference caBundle Secrets through ContainerCluster.spec.install.additionalTrustBundleRefs or its kind defaults; connection trust remains on the consuming service or Entitlement"
		case "machineAccess":
			return "Environment machineAccess is retired; use spec.remoteMachinesAccessKey.keyRef"
		case "safety":
			return "Environment safety is retired; authorize data loss per command with --authorize data-loss on apply or destroy"
		}
	}
	return ""
}

func allowedTag(n *desiredstate.Node) bool {
	switch n.Kind {
	case desiredstate.MappingKind:
		return n.Tag == "!!map"
	case desiredstate.SequenceKind:
		return n.Tag == "!!seq"
	case desiredstate.ScalarKind:
		return n.Tag == "!!str" || n.Tag == "!!bool" || n.Tag == "!!int" || n.Tag == "!!float" || n.Tag == "!!null"
	}
	return false
}

func leadingZero(n *desiredstate.Node) bool {
	return n.Kind == desiredstate.ScalarKind && n.Style == desiredstate.PlainStyle && (!n.ExplicitTag || n.Tag == "!!int") && leadingZeroInteger.MatchString(n.Value)
}

func scalarType(n *desiredstate.Node) api.ValueType {
	if n == nil || n.Kind != desiredstate.ScalarKind || leadingZero(n) {
		return api.Absent
	}
	if n.ExplicitTag {
		switch n.Tag {
		case "!!str":
			return api.String
		case "!!bool":
			if n.Style == desiredstate.PlainStyle && (n.Value == "true" || n.Value == "false") {
				return api.Boolean
			}
		case "!!int":
			if n.Style == desiredstate.PlainStyle && decimalInteger.MatchString(n.Value) {
				return api.Integer
			}
		case "!!float":
			if n.Style == desiredstate.PlainStyle && decimalNumber.MatchString(n.Value) {
				return api.Number
			}
		}
		return api.Absent
	}
	if n.Style != desiredstate.PlainStyle {
		return api.String
	}
	if n.Value == "true" || n.Value == "false" {
		return api.Boolean
	}
	if decimalInteger.MatchString(n.Value) {
		return api.Integer
	}
	if decimalNumber.MatchString(n.Value) {
		return api.Number
	}
	if n.Tag == "!!str" {
		return api.String
	}
	return api.Absent
}

func acceptsNode(shape *api.Shape, n *desiredstate.Node) bool {
	if n.Kind == desiredstate.MappingKind {
		return shape.Type == api.Mapping
	}
	if n.Kind == desiredstate.SequenceKind {
		return shape.Type == api.Sequence
	}
	t := scalarType(n)
	return t == shape.Type || shape.Type == api.Number && t == api.Integer
}
