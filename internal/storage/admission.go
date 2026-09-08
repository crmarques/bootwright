package storage

import (
	"fmt"
	"math/big"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type admission struct {
	object  api.Object
	catalog api.Catalog
	issues  []api.Issue
}

func isStorage(kind api.Kind) bool {
	return slices.Contains([]api.Kind{api.StorageCluster, api.StoragePlacementPolicy, api.StoragePool, api.StorageFilesystem, api.StorageObjectGateway, api.StorageNFSExport, api.StorageExport}, kind)
}

func (a *admission) issue(code, field, message, remediation string) {
	if len(a.issues) < 999 {
		a.issues = append(a.issues, api.Issue{Code: code, Field: field, Message: message, Remediation: remediation})
	}
}

func (a *admission) invalid(field, message string) {
	a.issue("api.invariant", field, message, "make the declaration consistent with its selected storage owner")
}

func (a *admission) required(value api.Value, key, field string) {
	if !value.Has(key) {
		a.issue("api.required", field+"."+key, "the selected storage mode requires this field", "supply the required field for the selected mode")
	}
}

func (a *admission) forbid(value api.Value, field string, keys ...string) {
	for _, key := range keys {
		if value.Has(key) {
			a.invalid(field+"."+key, "this field is forbidden for the selected storage mode")
		}
	}
}

func textOf(value api.Value, path ...string) string { return value.Get(path...).Text() }
func fallback(value api.Value, fallback string) string {
	if value.Present() {
		return value.Text()
	}
	return fallback
}
func numeric(value api.Value) *big.Int {
	if value.Type() != api.Integer {
		return new(big.Int)
	}
	n, ok := new(big.Int).SetString(value.Text(), 10)
	if !ok {
		return new(big.Int)
	}
	return n
}
func positive(value api.Value) bool               { return numeric(value).Sign() > 0 }
func floatPositive(value api.Value) bool          { n, ok := value.Float64(); return ok && n > 0 }
func indexed(field string, index int) string      { return fmt.Sprintf("%s[%d]", field, index) }
func contains(value api.Value, token string) bool { return slices.Contains(value.Strings(), token) }
func hasRole(node api.Value, role string) bool    { return contains(node.Get("roles"), role) }
func lookupNode(cluster api.Object, name string) (api.Value, bool) {
	var result api.Value
	count := 0
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		if textOf(node, "name") == name && name != "" {
			result = node
			count++
		}
	}
	return result, count == 1
}
func (a *admission) owner() (api.Object, bool) {
	return a.catalog.Find(api.StorageCluster, textOf(a.object.Spec(), "clusterRef"))
}
func (a *admission) sameOwner(kind api.Kind, name, field string, owner api.Object) (api.Object, bool) {
	target, ok := a.catalog.Find(kind, name)
	if !ok {
		return api.Object{}, false
	}
	if textOf(target.Spec(), "clusterRef") != owner.Name() {
		a.invalid(field, "referenced storage child belongs to a different cluster")
		return target, false
	}
	return target, true
}
func (a *admission) entitlement(value api.Value, path, expected string) {
	if target, ok := a.catalog.Find(api.Entitlement, value.Text()); ok && textOf(target.Spec(), "type") != "" && textOf(target.Spec(), "type") != expected {
		a.invalid(path, "entitlement product does not match this storage consumer")
	}
}

func ValidateAuthored(object api.Object, catalog api.Catalog) []api.Issue {
	if !isStorage(object.Kind()) && !cephProduct(object) {
		return nil
	}
	a := admission{object: object, catalog: catalog}
	if cephProduct(object) {
		a.cephEntitlement()
		return a.issues
	}
	a.lexicalValues()
	return a.issues
}

func Validate(object api.Object, catalog api.Catalog) []api.Issue {
	if !isStorage(object.Kind()) && !cephProduct(object) {
		return nil
	}
	a := admission{object: object, catalog: catalog}
	if cephProduct(object) {
		a.cephEntitlement()
		return a.issues
	}
	a.lexicalValues()
	if object.Kind() == api.StorageCluster {
		a.cluster()
		return a.issues
	}
	owner, ok := a.owner()
	if !ok {
		return a.issues
	}
	managed := fallback(owner.Spec().Get("management"), "managed") == "managed"
	if !managed && object.Kind() != api.StorageExport {
		a.invalid("$.spec.clusterRef", "this storage child requires a managed cluster")
		return a.issues
	}
	switch object.Kind() {
	case api.StoragePlacementPolicy:
		a.replication(owner, api.Object{})
	case api.StoragePool:
		a.pool(owner)
	case api.StorageFilesystem:
		a.filesystem(owner)
	case api.StorageObjectGateway:
		a.gateway(owner)
	case api.StorageNFSExport:
		a.nfs(owner)
	case api.StorageExport:
		a.export(owner, managed)
	}
	if managed {
		a.sharedServiceIdentities(owner)
	}
	return a.issues
}

func (a *admission) uniqueValues(values []string, field, message string) {
	seen := map[string]bool{}
	for i, value := range values {
		if value == "" {
			continue
		}
		if seen[value] {
			a.invalid(indexed(field, i), message)
		}
		seen[value] = true
	}
}

func nonemptyStrings(value api.Value) bool {
	for _, item := range value.Items() {
		if strings.TrimSpace(item.Text()) != "" {
			return true
		}
	}
	return false
}
