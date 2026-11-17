package ceph

import (
	"regexp"
	"slices"
	"strings"
)

type ReadinessEvidence struct {
	Checks      []string   `json:"checks"`
	Filesystems []string   `json:"filesystems"`
	License     string     `json:"license"`
	PGs         PGEvidence `json:"pgs"`
	Pools       []string   `json:"pools"`
	Scoped      []string   `json:"scoped"`
	Unnamed     bool       `json:"unnamed"`
}

type PGEvidence struct {
	Clean int64 `json:"clean"`
	Total int64 `json:"total"`
}

const healthRGWRoot = ".rgw.root"

var (
	toleratedHealth = []string{"CHA_ERROR_SENDING_REPORT"}
	healthCheckCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	healthPoolName  = regexp.MustCompile(`^[a-zA-Z0-9_.][a-zA-Z0-9_.-]{0,127}$`)
	healthFSName    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	licenseStates   = []string{"", "accepted", "unneeded", "pending"}
)

func validReadiness(e Evidence) bool {
	g := e.Readiness
	if g.Checks == nil || g.Scoped == nil || g.Pools == nil || g.Filesystems == nil || len(g.Checks) > 256 || len(g.Scoped) > len(g.Checks) ||
		len(g.Pools) > 4096 || len(g.Filesystems) > 4096 || !slices.Contains(licenseStates, g.License) || g.License != "" && e.Action != "cluster" ||
		g.PGs.Total < 0 || g.PGs.Clean < 0 || g.PGs.Clean > g.PGs.Total {
		return false
	}
	return sortedUnique(g.Checks, healthCheckCode) && sortedUnique(g.Scoped, healthCheckCode) && sortedUnique(g.Pools, healthPoolName) &&
		sortedUnique(g.Filesystems, healthFSName) && subset(g.Scoped, g.Checks)
}

func sortedUnique(values []string, grammar *regexp.Regexp) bool {
	for i, value := range values {
		if !grammar.MatchString(value) || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func subset(values, of []string) bool {
	for _, value := range values {
		if !slices.Contains(of, value) {
			return false
		}
	}
	return true
}

func readinessGates(e Evidence, r Request) bool {
	g := e.Readiness
	license := r.Action != "cluster" || g.License == "accepted" || g.License == "unneeded"
	pgs := g.PGs.Clean == g.PGs.Total && (g.PGs.Total > 0 || r.Action == "cluster")
	return license && pgs && healthReady(e, r)
}

func healthReady(e Evidence, r Request) bool {
	if e.Health == "HEALTH_OK" {
		return true
	}
	g := e.Readiness
	if g.Unnamed || len(g.Checks) == 0 {
		return false
	}
	for _, check := range g.Checks {
		if !slices.Contains(toleratedHealth, check) && !slices.Contains(g.Scoped, check) {
			return false
		}
	}
	pools, filesystems := healthOwned(r)
	for _, pool := range g.Pools {
		if slices.Contains(pools, pool) || strings.HasPrefix(pool, ".") && pool != healthRGWRoot {
			return false
		}
	}
	for _, filesystem := range g.Filesystems {
		if slices.Contains(filesystems, filesystem) {
			return false
		}
	}
	return true
}

func healthOwned(r Request) (pools, filesystems []string) {
	switch {
	case r.Action == "pool" && r.Pool != nil:
		pools = []string{r.Pool.Name}
	case r.Action == "rbd" && r.RBD != nil:
		pools = []string{r.RBD.Pool, r.RBD.DataPool}
	case r.Action == "filesystem" && r.Filesystem != nil:
		pools, filesystems = append([]string{r.Filesystem.MetadataPool}, r.Filesystem.DataPools...), []string{r.Filesystem.Name}
	case r.Action == "subvolume" && r.Subvolume != nil:
		pools, filesystems = []string{r.Subvolume.Pool}, []string{r.Subvolume.Filesystem}
	case r.Action == "gateway" && r.Gateway != nil:
		pools = append([]string{r.Gateway.IndexPool, r.Gateway.DataPool, r.Gateway.DataExtraPool, healthRGWRoot}, r.Gateway.ZonePools...)
	case r.Action == "client" && r.Client != nil:
		pools, filesystems = append([]string{r.Client.Pool}, r.Client.DataPools...), []string{r.Client.Filesystem}
	case r.Action == "nfs-export" && r.NFSExport != nil:
		filesystems = []string{r.NFSExport.Filesystem}
	case r.Action == "nvmeof" && r.NVMeOF != nil:
		pools = []string{r.NVMeOF.Pool}
		for _, namespace := range r.NVMeOF.Namespaces {
			pools = append(pools, namespace.Image.Pool, namespace.Image.DataPool)
		}
	}
	return slices.DeleteFunc(pools, empty), slices.DeleteFunc(filesystems, empty)
}

func empty(value string) bool { return value == "" }
