package lifecycle

import "slices"

func SecretNames(references ...string) []string {
	names := slices.DeleteFunc(slices.Clone(references), func(name string) bool { return name == "" })
	slices.Sort(names)
	return slices.Compact(names)
}
