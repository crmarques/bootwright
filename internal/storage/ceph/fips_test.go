package ceph

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func fipsCatalog() api.Catalog {
	objects := poolRequestCatalog(3, 3).Objects()
	for i, object := range objects {
		if object.Kind() == api.StorageCluster {
			objects[i] = object.WithSpec(object.Spec().WithPath(requestMap("fips", requestMap("enabled", true), "cephx", requestMap("keyType", "aes")), "ceph", "security"))
		}
	}
	objects = append(objects, api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, requestMap("customizations", requestMap("security", requestMap("fips", requestMap("enabled", true))))))
	objects = append(objects, api.NewObject(api.Secret, "cluster-key", api.Value{}, requestMap("source", requestMap("generated", requestMap("keyType", "ecdsa-p256")))))
	return api.NewCatalog(objects)
}

func TestFIPSPlanningFreezesPolicyAndRefusesUnsafeInputs(t *testing.T) {
	catalog := fipsCatalog()
	cluster, _ := catalog.Find(api.StorageCluster, "example")
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil || r.Security == nil || !r.Security.FIPS || r.Security.CephXKeyType != "aes" {
		t.Fatalf("FIPS request: %+v %v", r.Security, err)
	}
	if scopeOf(r).Security != r.Security {
		t.Fatal("host scope lost FIPS policy")
	}
	for _, kind := range []string{"aes256k", "unknown"} {
		changed := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue(kind), "ceph", "security", "cephx", "keyType"))
		if _, err := baseRequest(catalog, changed, "synthetic"); err == nil {
			t.Fatalf("admitted %s", kind)
		}
	}
	for _, change := range []string{"profile", "key"} {
		objects := catalog.Objects()
		for i, object := range objects {
			if change == "profile" && object.Kind() == api.MachineInstallProfile {
				objects[i] = object.WithSpec(api.MapValue())
			}
			if change == "key" && object.Kind() == api.Secret {
				objects[i] = object.WithSpec(requestMap("source", requestMap("generated", requestMap("keyType", "ed25519"))))
			}
		}
		if _, err := baseRequest(api.NewCatalog(objects), cluster, "synthetic"); err == nil {
			t.Fatalf("admitted unsafe %s", change)
		}
	}
}

type fipsProofs struct {
	dependencyProofs
	machines []string
}

func (p *fipsProofs) FIPS(_ []lifecycle.BlockEvidence, machine, _ string) error {
	p.machines = append(p.machines, machine)
	return errors.New("unproved installation")
}

func TestFIPSProofRefusesBeforeRunnerOrMaterial(t *testing.T) {
	r := runtimeRequest()
	r.Security = &Security{FIPS: true, CephXKeyType: "aes"}
	proof := &fipsProofs{}
	_, files, cleanup, err := boundMaterial(lifecycle.Execution{}, r, "apply", proof)
	defer cleanup()
	if err == nil || !strings.Contains(err.Error(), "unproved installation") || len(files) != 0 || len(proof.machines) != 1 {
		t.Fatalf("unproved host reached material: %v %v", files, err)
	}
}

func TestFIPSImportedEd25519RefusesBeforeHostPreparation(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-2")
	r.Cluster.Security = &Security{FIPS: true, CephXKeyType: "aes"}
	var err error
	e.Block.Request, err = r.canonical()
	if err != nil {
		t.Fatal(err)
	}
	e.Block.RequestDigest, err = reconciliation.RequestDigest(e.Block.BlockDefinition)
	if err != nil {
		t.Fatal(err)
	}
	runner := &capturingRunner{}
	if _, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e); err == nil || runner.calls != 0 {
		t.Fatalf("unsafe key reached host preparation: %v, calls %d", err, runner.calls)
	}
}
