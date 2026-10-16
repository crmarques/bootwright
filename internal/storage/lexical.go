package storage

import (
	"math/big"
	"regexp"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var (
	exactOSS      = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	checksum      = regexp.MustCompile(`(?i)^(?:sha256:)?[0-9a-f]{64}$`)
	retentionTime = regexp.MustCompile(`^([0-9]+)(y|w|d|h|m|s)$`)
	retentionSize = regexp.MustCompile(`^([0-9]+)(B|KB|MB|GB|TB|PB|EB)$`)
	storageSize   = regexp.MustCompile(`^([0-9]+)(B|K|M|G|T|P|E|KB|MB|GB|TB|PB|EB)?$`)
	deviceSize    = regexp.MustCompile(`^([0-9]+)(M|G|T|MB|GB|TB)$`)
	fileMode      = regexp.MustCompile(`^0?[0-7]{3}$`)
)

func quantity(value string, grammar *regexp.Regexp) (*big.Int, bool) {
	match := grammar.FindStringSubmatch(value)
	if match == nil {
		return nil, false
	}
	n, ok := new(big.Int).SetString(match[1], 10)
	return n, ok && n.Sign() > 0
}
func sizeBound(value string) (*big.Int, bool) {
	n, ok := quantity(value, deviceSize)
	if !ok {
		return nil, false
	}
	unit := deviceSize.FindStringSubmatch(value)[2]
	power := 2
	if strings.HasPrefix(unit, "G") {
		power = 3
	}
	if strings.HasPrefix(unit, "T") {
		power = 4
	}
	return n.Lsh(n, uint(power*10)), true
}
func validDeviceRange(value string) bool {
	if strings.Count(value, ":") == 0 {
		_, ok := sizeBound(value)
		return ok
	}
	if strings.Count(value, ":") != 1 {
		return false
	}
	lower, upper, _ := strings.Cut(value, ":")
	if lower == "" && upper == "" {
		return false
	}
	var low, high *big.Int
	var ok bool
	if lower != "" {
		low, ok = sizeBound(lower)
		if !ok {
			return false
		}
	}
	if upper != "" {
		high, ok = sizeBound(upper)
		if !ok {
			return false
		}
	}
	return low == nil || high == nil || low.Cmp(high) <= 0
}
func (a *admission) lexical(value api.Value, field string, valid bool) {
	if value.Type() == api.String && !valid {
		a.issue("api.value", field, "value does not match the storage field's lexical grammar", "use the field's documented complete lexical form")
	}
}
func (a *admission) size(value api.Value, field string, grammar *regexp.Regexp) {
	_, ok := quantity(value.Text(), grammar)
	a.lexical(value, field, ok)
}
func (a *admission) lexicalValues() {
	spec := a.object.Spec()
	if a.object.Kind() == api.StorageCluster {
		ceph := spec.Get("ceph")
		for _, path := range [][]string{{"release"}, {"packageVersion"}, {"cephadm", "ansible", "packageVersion"}, {"nfsGanesha", "packageVersion"}, {"image", "version"}} {
			value := ceph.Get(path...)
			a.lexical(value, "$.spec.ceph."+strings.Join(path, "."), NativeVersion(value.Text()))
		}
		a.size(ceph.Get("monitoring", "prometheus", "retentionTime"), "$.spec.ceph.monitoring.prometheus.retentionTime", retentionTime)
		a.size(ceph.Get("monitoring", "prometheus", "retentionSize"), "$.spec.ceph.monitoring.prometheus.retentionSize", retentionSize)
		for i, node := range ceph.Get("topology", "nodes").Items() {
			a.osdLexical(node.Get("osd"), indexed("$.spec.ceph.topology.nodes", i)+".osd")
		}
		for i, group := range ceph.Get("topology", "osdDrivegroups").Items() {
			a.osdLexical(group.Get("osd"), indexed("$.spec.ceph.topology.osdDrivegroups", i)+".osd")
		}
	}
	if a.object.Kind() == api.StoragePool {
		a.size(spec.Get("autoscale", "targetSizeBytes"), "$.spec.autoscale.targetSizeBytes", storageSize)
	}
	if a.object.Kind() == api.StorageFilesystem {
		for i, group := range spec.Get("subvolumeGroups").Items() {
			a.lexical(group.Get("mode"), indexed("$.spec.subvolumeGroups", i)+".mode", fileMode.MatchString(textOf(group, "mode")))
		}
	}
}
func (a *admission) osdLexical(osd api.Value, field string) {
	for _, key := range []string{"blockDBSize", "blockWALSize"} {
		a.size(osd.Get(key), field+"."+key, storageSize)
	}
	for _, key := range []string{"dataDevices", "dbDevices", "walDevices"} {
		value := osd.Get(key, "size")
		a.lexical(value, field+"."+key+".size", validDeviceRange(value.Text()))
	}
}

// NativeVersion bounds transport without interpreting a provider's version grammar.
func NativeVersion(value string) bool {
	return len(value) > 0 && len(value) <= 1024 && strings.IndexFunc(value, func(r rune) bool { return r < 32 || r == 127 }) < 0
}
