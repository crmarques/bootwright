package main

import (
	"context"
	"path/filepath"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestPackageRepositoryTrustBundleMustBeACABundleSecret(t *testing.T) {
	sources := admissibleExampleSources(t, false)
	replaced := 0
	for i, file := range sources.Files {
		if filepath.Base(file.Path()) != admissibleCephSources {
			continue
		}
		body := mustReplaceOnce(t, file.Path(), string(file.Bytes()), "packageRepositoryTrustBundleRef: custom-ca-bundle\n", "packageRepositoryTrustBundleRef: ceph-registry-credentials\n")
		sources.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(body))
		replaced++
	}
	if replaced != 1 {
		t.Fatalf("the projection holds %d Ceph sources, want one", replaced)
	}
	state, _, err := wireCompiler().Compile(context.Background(), sources)
	if err == nil || state != nil {
		t.Fatal("a usernamePassword Secret was admitted as the package repository trust bundle")
	}
	for _, d := range diagnostics.Of(err) {
		if d.Code == "api.reference" && d.Field == "$.spec.packageRepositoryTrustBundleRef" && d.Object != nil &&
			d.Object.Kind == string(api.CephCluster) && d.Object.Name == "ceph-multidc-01" &&
			d.Message == "Secret/ceph-registry-credentials is declared as usernamePassword; this field accepts caBundle" {
			return
		}
	}
	t.Fatal("no Secret-type refusal at the package repository trust bundle", diagnostics.Of(err))
}
