package catalog

import (
	"context"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Service struct{ registry addons.Registry }

func New(registry addons.Registry) Service { return Service{registry: registry} }

type ListRequest struct{}
type ListResult struct{ AddOns []Entry }
type Entry struct {
	Name                      string
	Provides, Requires        []string
	OLMPackage, DefaultSource string
	Configuration             []string
}

type DescribeRequest struct{ Name string }
type DescribeResult struct{ Package Description }
type Description struct {
	Name, Summary             string
	Provides, Requires        []string
	OLMPackage, DefaultSource string
	Namespaces                []string
	Lookup                    string
	Configuration             []Field
	Binding                   string
}
type Field struct {
	Path, Type     string
	Required       bool
	RequiredWithin string
	Default        string
	Values         []string
}

func (s Service) List(ctx context.Context, _ ListRequest) (*ListResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &ListResult{AddOns: []Entry{}}
	for _, p := range s.registry.Packages() {
		row := Entry{Name: p.Name, Provides: slices.Clone(p.Provides), Requires: slices.Clone(p.Requires), Configuration: []string{}}
		if p.OLM != nil {
			row.OLMPackage = p.OLM.Package
			row.DefaultSource = p.OLM.DefaultSource
		}
		for _, field := range memberShape(p.Name).Fields {
			row.Configuration = append(row.Configuration, field.Name)
		}
		slices.Sort(row.Configuration)
		result.AddOns = append(result.AddOns, row)
	}
	return result, nil
}

func (s Service) Describe(ctx context.Context, request DescribeRequest) (*DescribeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, ok := s.registry.Find(request.Name)
	if !ok {
		return nil, diagnostics.NewFailureWithRemediation("access.target", "no built-in add-on package is named "+strconv.Quote(request.Name), "", "name a package that bootwright add-ons list shows")
	}
	description := Description{Name: p.Name, Summary: p.Summary, Provides: slices.Clone(p.Provides), Requires: slices.Clone(p.Requires), Namespaces: []string{}, Configuration: []Field{}}
	if p.OLM != nil {
		description.OLMPackage = p.OLM.Package
		description.DefaultSource = p.OLM.DefaultSource
		description.Lookup = "oc get packagemanifest " + p.OLM.Package + " -n openshift-marketplace -o json"
		description.Namespaces = append(description.Namespaces, p.OLM.Namespace)
	}
	for _, namespace := range p.PlacementNamespaces {
		if !slices.Contains(description.Namespaces, namespace) {
			description.Namespaces = append(description.Namespaces, namespace)
		}
	}
	slices.Sort(description.Namespaces)
	walk(memberShape(p.Name), "", "", &description.Configuration)
	for i, field := range description.Configuration {
		if field.Path == "subscription.source" && field.Default == "" {
			description.Configuration[i].Default = description.DefaultSource
		}
	}
	description.Binding = skeleton(p, description.Configuration)
	return &DescribeResult{Package: description}, nil
}

func memberShape(name string) *api.Shape {
	packages, _ := api.Schema(api.ClusterAddonBinding).Field("packages")
	if member, ok := packages.Shape.Field(name); ok {
		return member.Shape
	}
	return &api.Shape{}
}

func walk(shape *api.Shape, path, within string, out *[]Field) {
	for _, f := range shape.Fields {
		p := f.Name
		if path != "" {
			p = path + "." + f.Name
		}
		childWithin := within
		if !f.Required {
			childWithin = p
		}
		element := f.Shape.Element
		switch {
		case f.Shape.Type == api.Mapping && len(f.Shape.Fields) > 0:
			walk(f.Shape, p, childWithin, out)
		case f.Shape.Type == api.Sequence && element != nil && element.Type == api.Mapping && len(element.Fields) > 0:
			if !f.Required {
				childWithin = p + "[]"
			}
			walk(element, p+"[]", childWithin, out)
		default:
			*out = append(*out, leaf(f, p, within))
		}
	}
}

func leaf(f api.Field, path, within string) Field {
	field := Field{Path: path, Type: typeName(f.Shape), Required: f.Required && within == "", Values: slices.Clone(f.Shape.Enums)}
	if f.Required && within != "" {
		field.RequiredWithin = within
	}
	if f.Default.Present() {
		field.Default = f.Default.Text()
		if f.Default.Type() == api.Boolean {
			field.Default = strconv.FormatBool(f.Default.Bool())
		}
	}
	return field
}

func typeName(shape *api.Shape) string {
	switch {
	case len(shape.Reference) > 0:
		kinds := make([]string, 0, len(shape.Reference))
		for _, kind := range shape.Reference {
			kinds = append(kinds, string(kind))
		}
		return "name of " + strings.Join(kinds, " or ")
	case shape.Rule == "dns":
		return "DNS name"
	case shape.Rule == "image":
		return "image reference"
	case shape.Rule == "http-url":
		return "HTTP URL"
	case shape.Rule != "":
		return shape.Rule
	}
	switch shape.Type {
	case api.Boolean:
		return "boolean"
	case api.Integer:
		return "integer"
	case api.Number:
		return "number"
	case api.Mapping:
		return "mapping"
	case api.Sequence:
		if shape.Element != nil {
			return "list of " + typeName(shape.Element)
		}
		return "list"
	}
	return "string"
}

type node struct {
	name     string
	value    string
	children []*node
}

func (n *node) child(name string) *node {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	c := &node{name: name}
	n.children = append(n.children, c)
	return c
}

func (n *node) write(out *strings.Builder, depth int) {
	for _, c := range n.children {
		out.WriteString(strings.Repeat("  ", depth))
		out.WriteString(c.name + ":")
		if len(c.children) == 0 {
			out.WriteString(" " + c.value)
		}
		out.WriteString("\n")
		c.write(out, depth+1)
	}
}

func skeleton(p addons.Package, fields []Field) string {
	member := &node{}
	for _, f := range fields {
		defaulted := f.Path == "subscription.source" && p.OLM != nil && p.OLM.DefaultSource != ""
		if !f.Required && !defaulted {
			continue
		}
		parts := strings.Split(f.Path, ".")
		at := member
		for _, part := range parts {
			at = at.child(part)
		}
		at.value = "<" + parts[len(parts)-1] + ">"
		if defaulted {
			at.value = p.OLM.DefaultSource
		}
	}
	root := &node{}
	root.child("apiVersion").value = api.APIVersion
	root.child("kind").value = string(api.ClusterAddonBinding)
	root.child("metadata").child("name").value = p.Name
	spec := root.child("spec")
	spec.child("clusterRef").value = "<clusterRef>"
	packages := spec.child("packages")
	packages.children = append(packages.children, &node{name: p.Name, children: member.children})
	var out strings.Builder
	root.write(&out, 0)
	return out.String()
}
