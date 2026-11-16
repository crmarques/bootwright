package ceph

import (
	"context"
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestIdentityRefusesEveryForeignArm(t *testing.T) {
	arms := map[string]func(*Request){
		"settings":      func(r *Request) { r.Settings = &Settings{} },
		"service":       func(r *Request) { r.Service = &ServiceUnit{Type: "nfs", ID: "files"} },
		"gateway":       func(r *Request) { r.Gateway = &Gateway{Name: "object"} },
		"policy":        func(r *Request) { r.Policy = &Policy{Name: "object"} },
		"pool":          func(r *Request) { r.Pool = &Pool{Name: "object"} },
		"client":        func(r *Request) { r.Client = &Client{Name: "object"} },
		"rbd":           func(r *Request) { r.RBD = &RBDImage{Name: "object"} },
		"filesystem":    func(r *Request) { r.Filesystem = &Filesystem{Name: "object"} },
		"subvolume":     func(r *Request) { r.Subvolume = &Subvolume{Name: "object"} },
		"nvmeof":        func(r *Request) { r.NVMeOF = &NVMeOFSubsystem{Name: "object"} },
		"nfs-export":    func(r *Request) { r.NFSExport = &NFSExport{Name: "object"} },
		"object-user":   func(r *Request) { r.ObjectUser = &ObjectUser{Name: "object"} },
		"object-bucket": func(r *Request) { r.ObjectBucket = &ObjectBucket{Name: "object"} },
	}
	kinds := map[string]api.Kind{"cluster": api.CephCluster, "settings": api.CephCluster, "service": api.CephCluster, "gateway": api.CephObjectGateway, "policy": api.CephPlacementPolicy,
		"pool": api.CephPool, "client": api.CephClient, "rbd": api.CephRBDImage, "filesystem": api.CephFilesystem, "subvolume": api.CephSubvolume, "nvmeof": api.CephNVMeoFSubsystem,
		"nfs-export": api.CephNFSExport, "object-user": api.CephObjectUser, "object-bucket": api.CephObjectBucket}
	if len(arms) != 13 || len(kinds) != len(arms)+1 {
		t.Fatalf("the table covers %d arms and %d actions", len(arms), len(kinds))
	}
	for action, kind := range kinds {
		base := coreRequest()
		base.Action = action
		if own, ok := arms[action]; ok {
			own(&base)
		}
		capability := New(kind, nil)
		e := executionFor(t, base)
		e.Block.Kind = string(kind)
		switch action {
		case "cluster", "settings", "service":
		case "gateway":
			e.Block.ID, e.Block.Object = GatewayBlockID("object"), "object"
		default:
			e.Block.Object = "object"
		}
		if _, err := capability.identity(context.Background(), e); err != nil {
			t.Fatalf("a %s request with only its own arm fails its identity: %+v", action, diagnostics.Of(err))
		}
		for arm, add := range arms {
			if arm == action {
				continue
			}
			r := base
			add(&r)
			if _, err := r.Canonical(); err != nil {
				encoded, _ := json.Marshal(r)
				if _, err := DecodeRequest(encoded); err == nil {
					t.Errorf("a %s request carrying a %s arm cannot be frozen but decodes", action, arm)
				}
				continue
			}
			foreign := executionFor(t, r)
			foreign.Block.ID, foreign.Block.Kind, foreign.Block.Object = e.Block.ID, e.Block.Kind, e.Block.Object
			_, err := capability.identity(context.Background(), foreign)
			if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "lifecycle.state" || d[0].Message != "Ceph request does not match its frozen resource identity" {
				t.Errorf("a %s request carrying a %s arm passes its identity: %+v", action, arm, d)
			}
		}
	}
}
