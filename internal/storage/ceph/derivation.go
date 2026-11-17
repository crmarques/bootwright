package ceph

import (
	"reflect"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type derivation struct {
	context string
	plan    lifecycle.PlanInput
}

func planDerivation(input lifecycle.PlanInput) derivation {
	return derivation{context: input.Context.Name, plan: input}
}

func contextDerivation(contextName string) derivation { return derivation{context: contextName} }

func (d derivation) base(c api.Catalog, cluster api.Object) (Request, error) {
	shared, err := lifecycle.Derived(d.plan, "storage/ceph.base/"+cluster.Name(), func() (Request, error) {
		return baseRequest(c, cluster, d.context)
	})
	if err != nil {
		return Request{}, err
	}
	return cloneRequest(shared), nil
}

func cloneRequest(r Request) Request {
	var clone Request
	deepCopy(reflect.ValueOf(&clone).Elem(), reflect.ValueOf(r))
	return clone
}

func deepCopy(to, from reflect.Value) {
	switch from.Kind() {
	case reflect.Pointer:
		if !from.IsNil() {
			to.Set(reflect.New(from.Elem().Type()))
			deepCopy(to.Elem(), from.Elem())
		}
	case reflect.Slice:
		if !from.IsNil() {
			to.Set(reflect.MakeSlice(from.Type(), from.Len(), from.Len()))
			for i := range from.Len() {
				deepCopy(to.Index(i), from.Index(i))
			}
		}
	case reflect.Map:
		if !from.IsNil() {
			to.Set(reflect.MakeMapWithSize(from.Type(), from.Len()))
			for entry := from.MapRange(); entry.Next(); {
				value := reflect.New(entry.Value().Type()).Elem()
				deepCopy(value, entry.Value())
				to.SetMapIndex(entry.Key(), value)
			}
		}
	case reflect.Struct:
		for i := range from.NumField() {
			deepCopy(to.Field(i), from.Field(i))
		}
	case reflect.Interface:
		if !from.IsNil() {
			value := reflect.New(from.Elem().Type()).Elem()
			deepCopy(value, from.Elem())
			to.Set(value)
		}
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.String:
		to.Set(from)
	default:
		panic("storage/ceph: a request field of kind " + from.Kind().String() + " cannot be copied deeply")
	}
}
