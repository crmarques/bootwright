package compilation

import (
	"errors"
	"math"
	"regexp"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

var decimalInteger = regexp.MustCompile(`^[+-]?[0-9]+$`)
var decimalNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

type decoder struct {
	document  desiredstate.Document
	sink      *diagnosticSink
	locations map[string]diagnostics.SourceLocation
	failed    bool
}

func decodeDocument(document desiredstate.Document, sink *diagnosticSink) *objectRecord {
	if document.IsEmpty() {
		return nil
	}
	d := decoder{document: document, sink: sink, locations: map[string]diagnostics.SourceLocation{}}
	root := documentBody(document)
	if root == nil || root.Kind != desiredstate.MappingKind {
		d.fail(root, "yaml.shape", "$", "a desired-state document must be a mapping")
		return nil
	}
	kindNode := mappingNode(root, "kind")
	if kindNode == nil {
		d.fail(root, "api.kind", "$.kind", "kind must name a registered API kind")
		return nil
	}
	kindValue := d.value(kindNode, &api.Shape{Type: api.String}, "$.kind")
	if d.failed {
		return nil
	}
	kind := api.Kind(kindValue.Text())
	shape := api.Schema(kind)
	if shape == nil {
		message := "kind must name a registered API kind"
		if kind == "InfraComponent" {
			message = "InfraComponent is retired; declare Proxy, DNSServer, NTPServer, ArtifactServer, Registry, or LoadBalancer with management on its spec"
		}
		d.fail(mappingNode(root, "kind"), "api.kind", "$.kind", message)
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
		d.fail(mappingNode(root, "apiVersion"), "api.version", "$.apiVersion", "apiVersion must be bootwright.io/v1alpha1")
	}
	for _, key := range []string{"metadata", "spec"} {
		if mappingNode(root, key) == nil {
			d.fail(root, "api.required", "$."+key, "required envelope field is absent")
		}
	}
	if value.Has("metadata") && mappingNode(mappingNode(root, "metadata"), "name") == nil {
		d.fail(mappingNode(root, "metadata"), "api.required", "$.metadata.name", "metadata.name is required")
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

func (d *decoder) fail(node *desiredstate.Node, code, field, message string) {
	d.failed = true
	location := diagnostics.SourceLocation{Path: d.document.Path, Document: d.document.Index}
	if node != nil {
		location.Line = node.Line
		location.Column = node.Column
	}
	d.sink.add(diagnostics.Diagnostic{Severity: "error", Code: code, Message: message, Source: &location, Field: field})
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
		if !matched {
			d.fail(node, "api.type", path, "field requires a different YAML type")
			return api.Value{}
		}
	}
	switch node.Kind {
	case desiredstate.MappingKind:
		if shape.Type != api.Absent && shape.Type != api.Mapping {
			d.fail(node, "api.type", path, "field requires a different YAML type")
			return api.Value{}
		}
		fields := []api.FieldValue{}
		seen := map[string]bool{}
		for i := 0; i+1 < len(node.Content) && !d.sink.stopped(); i += 2 {
			key, n := node.Content[i], node.Content[i+1]
			if key.Kind == desiredstate.ScalarKind && key.Value == "<<" && (key.Tag == "!!merge" || scalarType(key) == api.String) {
				d.fail(key, "yaml.alias", path, "merge keys are not permitted")
				continue
			}
			if d.refusedConstruct(key, path) {
				continue
			}
			if key.Kind != desiredstate.ScalarKind || scalarType(key) != api.String {
				d.fail(key, "yaml.shape", path, "mapping keys must be strings")
				continue
			}
			if seen[key.Value] {
				d.fail(key, "yaml.duplicate-key", path, "mapping keys must be unique")
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
					d.fail(key, "api.field", path, "default kind is not registered")
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
				if message := d.retiredFieldMessage(path, key.Value); message != "" {
					d.fail(key, "api.field", childPath, message)
				} else {
					d.fail(key, "api.field", path, "field is not permitted by this schema")
				}
				continue
			}
			fields = append(fields, api.FieldValue{Name: key.Value, Value: d.value(n, childShape, childPath)})
		}
		return api.MapValue(fields...)
	case desiredstate.SequenceKind:
		if shape.Type != api.Absent && shape.Type != api.Sequence {
			d.fail(node, "api.type", path, "field requires a different YAML type")
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
	case desiredstate.ScalarKind:
		kind := scalarType(node)
		if node.Tag == "!!null" && !(node.ExplicitTag && node.Tag == "!!str") {
			d.fail(node, "api.type", path, "null values are not permitted")
			return api.Value{}
		}
		if kind == api.Absent {
			d.fail(node, "api.type", path, "scalar spelling or YAML type is not permitted")
			return api.Value{}
		}
		if shape.Type != api.Absent && kind != shape.Type && !(shape.Type == api.Number && kind == api.Integer) {
			d.fail(node, "api.type", path, "field requires a different YAML scalar type")
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
				d.fail(node, "api.type", path, "number must be finite")
				return api.Value{}
			}
			return api.NumberValue(strconv.FormatFloat(n, 'g', -1, 64))
		}
	}
	d.fail(node, "yaml.shape", path, "unsupported YAML representation")
	return api.Value{}
}

func (d *decoder) refusedConstruct(node *desiredstate.Node, path string) bool {
	if node.Kind == desiredstate.AliasKind || node.Anchor != "" {
		d.fail(node, "yaml.alias", path, "anchors and aliases are not permitted")
		return true
	}
	if node.ExplicitTag && !allowedTag(node) {
		d.fail(node, "yaml.tag", path, "the YAML tag is not permitted")
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

func scalarType(n *desiredstate.Node) api.ValueType {
	if n == nil || n.Kind != desiredstate.ScalarKind {
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
