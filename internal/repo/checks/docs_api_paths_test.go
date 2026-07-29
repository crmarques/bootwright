package repocheck

import (
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var authoredPathPattern = regexp.MustCompile("`(spec\\.[a-zA-Z0-9_.\\[\\]]*[a-zA-Z0-9_\\]])`")

var authoredPathAllow = map[string]bool{
	"spec.controlPlane": true,
	"spec.compute[]":    true,
}

func specTypes(t *testing.T) []reflect.Type {
	t.Helper()
	stateType := reflect.TypeOf(v1alpha1.State{})
	var out []reflect.Type
	for i := 0; i < stateType.NumField(); i++ {
		elem := stateType.Field(i).Type
		if elem.Kind() == reflect.Slice {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			continue
		}
		spec, ok := elem.FieldByName("Spec")
		if !ok {
			continue
		}
		out = append(out, spec.Type)
	}
	return out
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	return t
}

func resolveAuthoredPath(root reflect.Type, segments []string) bool {
	cur := deref(root)
	for i, seg := range segments {
		if idx := strings.IndexByte(seg, '['); idx >= 0 {
			seg = seg[:idx]
		}
		if seg == "" {
			return false
		}
		if cur.Kind() == reflect.Map || cur.Kind() == reflect.Interface {
			return true
		}
		if cur.Kind() != reflect.Struct {
			return false
		}
		field, ok := structFieldByYAMLTag(cur, seg)
		if !ok {
			return false
		}
		next := deref(field.Type)
		if i == len(segments)-1 {
			return true
		}
		cur = next
	}
	return true
}

func structFieldByYAMLTag(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			if inner, ok := structFieldByYAMLTag(deref(f.Type), name); ok {
				return inner, true
			}
			continue
		}
		if yamlTagName(f) == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

func authoredPathExists(specs []reflect.Type, path string) bool {
	segments := strings.Split(strings.TrimPrefix(path, "spec."), ".")
	if len(segments) == 0 {
		return false
	}
	for _, s := range specs {
		if resolveAuthoredPath(s, segments) {
			return true
		}
	}
	return false
}

func TestDocumentedAuthoredPathsExistInAPI(t *testing.T) {
	specs := specTypes(t)
	if len(specs) == 0 {
		t.Fatal("no authored spec types discovered from v1alpha1.State")
	}
	sources := []string{"specs/state-model.md"}
	root := repoRoot(t)
	conceptPages, err := filepath.Glob(filepath.Join(root, "docs", "concepts", "*.md"))
	if err != nil {
		t.Fatalf("glob docs/concepts: %v", err)
	}
	for _, page := range conceptPages {
		rel, relErr := filepath.Rel(root, page)
		if relErr != nil {
			t.Fatalf("relpath %s: %v", page, relErr)
		}
		sources = append(sources, rel)
	}
	sort.Strings(sources)

	missing := map[string][]string{}
	for _, src := range sources {
		body := readRepoFile(t, src)
		for _, match := range authoredPathPattern.FindAllStringSubmatch(body, -1) {
			path := match[1]
			if authoredPathAllow[path] || strings.HasSuffix(path, ".") {
				continue
			}
			if authoredPathExists(specs, path) {
				continue
			}
			missing[path] = append(missing[path], src)
		}
	}
	if len(missing) == 0 {
		return
	}
	paths := make([]string, 0, len(missing))
	for p := range missing {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		t.Errorf("documented authored path %q resolves to no field in any api/v1alpha1 spec type (named in %s)", p, strings.Join(dedupeStrings(missing[p]), ", "))
	}
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
