package cli

import (
	"github.com/spf13/pflag"
)

type scalarValue struct {
	finalValue         string
	hasEmptyOccurrence bool
}

func (v *scalarValue) String() string { return v.finalValue }
func (v *scalarValue) Type() string   { return "string" }
func (v *scalarValue) Set(value string) error {
	v.finalValue = value
	v.hasEmptyOccurrence = v.hasEmptyOccurrence || value == ""
	return nil
}

func registerFlags(flags *pflag.FlagSet, specs []flagSpec) {
	for _, spec := range specs {
		if spec.required {
			spec.help += " (required)"
		}
		switch spec.kind {
		case "bool":
			flags.BoolP(spec.name, spec.short, spec.defaultValue == "true", spec.help)
		case "stringArray":
			flags.StringArrayP(spec.name, spec.short, nil, spec.help)
		default:
			flags.VarP(&scalarValue{finalValue: spec.defaultValue}, spec.name, spec.short, spec.help)
		}
		annotations := map[string][]string{}
		if len(spec.enum) > 0 {
			annotations["bootwright.enum"] = append([]string(nil), spec.enum...)
		}
		if spec.catalog != "" {
			annotations["bootwright.catalog"] = []string{spec.catalog}
		}
		if spec.path != "" {
			annotations["bootwright.path"] = []string{spec.path}
		}
		if spec.undisclosed {
			annotations[undisclosedAnnotation] = []string{"true"}
		}
		if len(annotations) > 0 {
			flags.Lookup(spec.name).Annotations = annotations
		}
	}
}

func stringValue(flags *pflag.FlagSet, name string) string {
	if flag := flags.Lookup(name); flag != nil {
		return flag.Value.String()
	}
	return ""
}

func boolValue(flags *pflag.FlagSet, name string) bool {
	value, _ := flags.GetBool(name)
	return value
}

func arrayValue(flags *pflag.FlagSet, name string) []string {
	value, _ := flags.GetStringArray(name)
	return value
}

func normalizeScalar(flags *pflag.FlagSet, name, value string) {
	if flag := flags.Lookup(name); flag != nil {
		if scalar, ok := flag.Value.(*scalarValue); ok {
			scalar.finalValue = value
		}
	}
}
