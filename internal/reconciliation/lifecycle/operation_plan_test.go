package lifecycle

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const secretsPackage = "github.com/crmarques/bootwright/internal/secrets"

func operationPlanAllowances() map[string]string {
	return map[string]string{}
}

func TestOperationPlanHoldsNoHandles(t *testing.T) {
	forbidden := map[reflect.Type]string{
		reflect.TypeFor[Transaction]():    "Transaction",
		reflect.TypeFor[OperationStore](): "OperationStore",
		reflect.TypeFor[os.File]():        "os.File",
		reflect.TypeFor[*os.File]():       "*os.File",
	}
	seen := map[reflect.Type]bool{}
	allowed := map[string]bool{}
	var walk func(kind reflect.Type, path string)
	walk = func(kind reflect.Type, path string) {
		if name, ok := forbidden[kind]; ok {
			t.Errorf("%s holds the %s handle", path, name)
			return
		}
		if pkg := kind.PkgPath(); pkg == secretsPackage || strings.HasPrefix(pkg, secretsPackage+"/") {
			t.Errorf("%s holds %s, a type of the secrets packages", path, kind)
			return
		}
		if kind == reflect.TypeFor[error]() {
			return
		}
		switch kind.Kind() {
		case reflect.Interface, reflect.Func, reflect.Chan:
			if _, ok := operationPlanAllowances()[path]; ok {
				allowed[path] = true
				return
			}
			t.Errorf("%s is a %s, which an operation plan may not hold", path, kind.Kind())
			return
		}
		if seen[kind] {
			return
		}
		seen[kind] = true
		switch kind.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(kind.Elem(), path+"[]")
		case reflect.Map:
			walk(kind.Key(), path+"[key]")
			walk(kind.Elem(), path+"[]")
		case reflect.Struct:
			for index := range kind.NumField() {
				field := kind.Field(index)
				walk(field.Type, path+"."+field.Name)
			}
		}
	}
	walk(reflect.TypeFor[operationPlan](), "operationPlan")
	for path := range operationPlanAllowances() {
		if !allowed[path] {
			t.Errorf("allowance %q no longer applies; remove it", path)
		}
	}
}
