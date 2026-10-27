package compilation

import (
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	OriginAuthored = "authored"
	OriginBuiltIn  = "built-in"
	OriginDerived  = "derived"
)

type FieldOrigin struct {
	Field  string `json:"field"`
	Origin string `json:"origin"`
}

func (r *objectRecord) fieldOrigins() []FieldOrigin {
	origins := []FieldOrigin{}
	var walk func(api.Value, string)
	walk = func(value api.Value, path string) {
		switch {
		case value.Type() == api.Mapping && value.Len() > 0:
			for _, field := range value.Fields() {
				walk(field.Value, path+"."+field.Name)
			}
		case value.Type() == api.Sequence && value.Len() > 0:
			for index, item := range value.Items() {
				walk(item, path+"["+strconv.Itoa(index)+"]")
			}
		default:
			origins = append(origins, FieldOrigin{Field: path, Origin: r.originOf(path)})
		}
	}
	walk(r.object.Spec(), "$.spec")
	return origins
}

func (r *objectRecord) originOf(path string) string {
	source := r.sourceField(path)
	switch {
	case valueAt(r.authored.Value(), source).Present():
		return OriginAuthored
	case valueAt(specEnvelope(r.inheritedSpec), source).Present():
		return "defaults." + string(r.object.Kind())
	case valueAt(specEnvelope(r.builtInSpec), source).Present():
		return OriginBuiltIn
	}
	return OriginDerived
}

func specEnvelope(spec api.Value) api.Value {
	return api.MapValue(api.FieldValue{Name: "spec", Value: spec})
}
