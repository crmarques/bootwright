package ceph

import (
	"fmt"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func poolRequest(c api.Catalog, pool api.Object, contextName string) (Request, error) {
	cluster, _ := c.Find(api.StorageCluster, pool.Spec().Get("clusterRef").Text())
	r, err := baseRequest(c, cluster, contextName)
	if err != nil {
		return Request{}, err
	}
	s := pool.Spec()
	if s.Get("type").Text() != "replicated" || s.Get("role").Text() != "rbd" || s.Has("placementPolicyRef") || s.Has("quota") || s.Has("compression") || s.Has("mirroring") {
		return Request{}, failure("lifecycle.unsupported", "the core pool adapter realizes only replicated RBD pools", "use a replicated RBD pool without later-feature policies")
	}
	size, ok := s.Get("replicated", "size").Int64()
	if !ok || size == 0 {
		size = 3
	}
	minSize, ok := s.Get("replicated", "minSize").Int64()
	if !ok || minSize == 0 {
		minSize = 2
	}
	if size < 2 || minSize < 2 || minSize > size {
		return Request{}, failure("api.invariant", "core pools require at least two replicas and minSize between two and size", "set replicated.size to at least 2 and replicated.minSize between 2 and replicated.size")
	}
	osdHosts := 0
	for _, host := range r.Hosts {
		if len(host.Devices) > 0 {
			osdHosts++
		}
	}
	if size >= int64(osdHosts) {
		return Request{}, failure("api.invariant", fmt.Sprintf("IBM replication size %d requires more than %d OSD-bearing hosts; %d are declared", size, size, osdHosts), "add OSD-bearing hosts, or explicitly select size 2 and minSize 2 for a three-host cluster; omitted or zero size retains the default of 3")
	}
	if s.Has("autoscale") && (s.Get("autoscale").Len() != 1 || s.Get("autoscale", "mode").Text() != "on") {
		return Request{}, failure("lifecycle.unsupported", "additional autoscaler settings require pool feature qualification", "use autoscale.mode: on or omit autoscale for the core profile")
	}
	if s.Has("application") && s.Get("application").Text() != "rbd" {
		return Request{}, failure("api.invariant", "RBD pool application must agree with its role", "set application to rbd")
	}
	r.Action = "pool"
	r.Pool = &Pool{Name: pool.Name(), Size: int(size), MinSize: int(minSize), PGNum: 32, CrushRule: "replicated_rule"}
	return r, nil
}

func clientRequest(c api.Catalog, client api.Object, contextName string) (Request, error) {
	pool, _ := c.Find(api.StoragePool, client.Spec().Get("poolRef").Text())
	r, err := poolRequest(c, pool, contextName)
	if err != nil {
		return Request{}, err
	}
	r.Action = "client"
	r.Pool = nil
	r.Client = &Client{Name: client.Name(), Pool: pool.Name(), Access: client.Spec().Get("access").Text()}
	return r, nil
}
