//go:build linux && amd64

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func TestRetiredServiceAndTrustSnapshotCanBeReplacedWithoutLosingSecrets(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	addSecretInput(t, input, "secret.yaml", secretDocument("retained-token", "token", "  source: {generated: {}}\n"))
	addSecretInput(t, input, "ca.yaml", secretDocument("install-ca", "caBundle", "  source: {file: {path: secrets/never-opened.pem}}\n"))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "generate", "--name", "retained-token")
	material, _ := contextRun(t, services, 0, "secret", "show", "--name", "retained-token", "--part", "value")
	registry, err := repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	original := registry.Contexts[0]

	legacy := strings.Replace(syntheticEnvironment, "  controller: {machineRef: service-host}\n", "  controller: {proxy: {proxyRef: egress}}\n", 1) + "  proxy: {defaultRef: egress}\n  infraComponents:\n    proxies:\n      - name: egress\n        management: external\n        connection: {httpProxy: 'http://proxy.example.test:3128'}\n  trustedCAs: {caBundleRefs: [install-ca]}\n"
	addSecretInput(t, input, "environment.yaml", legacy)
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{input})
	if err != nil {
		t.Fatal(err)
	}
	// Publish exact historical source bytes through the storage boundary to
	// model a snapshot admitted by the earlier compiler.
	err = repository.Transact(context.Background(), false, sources.Roots, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(context.Background(), original.Name); err != nil {
			return err
		}
		revision, err := tx.Publish(context.Background(), original.Name, input, sources)
		if err != nil {
			return err
		}
		reg := tx.Registry()
		reg.Contexts[0].Revision = revision
		return tx.Commit(context.Background(), reg)
	})
	if err != nil {
		t.Fatal(err)
	}
	before := stateFingerprint(t, root)
	for _, args := range [][]string{
		{"validate", "--context", "alpha", "--output", "json"},
		{"render", "effective", "--context", "alpha", "--output", "json"},
	} {
		out, stderr := contextRun(t, services, 1, args...)
		if stderr != "" || !strings.Contains(out, "api.field") || !strings.Contains(out, "retired") || !strings.Contains(out, "controller.proxy") || !strings.Contains(out, "Machine.spec.proxy") || !strings.Contains(out, "ContainerCluster.spec.install.additionalTrustBundleRefs") {
			t.Fatalf("retired input did not explain its replacement: %s %s", out, stderr)
		}
		if !sameFingerprints(before, stateFingerprint(t, root)) {
			t.Fatal("failed legacy inspection changed context state")
		}
	}

	addSecretInput(t, input, "environment.yaml", syntheticEnvironment+"  defaults:\n    ContainerCluster:\n      install:\n        additionalTrustBundleRefs: [install-ca]\n")
	addSecretInput(t, input, "controller.yaml", strings.Replace(serviceHost, "spec:\n", "spec:\n  proxy: {proxyRef: egress}\n", 1))
	addSecretInput(t, input, "proxy.yaml", "apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: egress}\nspec:\n  management: external\n  connection: {httpProxy: 'http://proxy.example.test:3128'}\n")
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	contextRun(t, services, 0, "validate", "--context", "alpha")
	registry, err = repository.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if registry.Contexts[0].Name != original.Name || registry.Contexts[0].Revision == original.Revision || registry.Contexts[0].EnvironmentDirectory != filepath.Clean(input) {
		t.Fatal("schema replacement did not preserve context identity and source directory")
	}
	restored, stderr := contextRun(t, services, 0, "secret", "show", "--name", "retained-token", "--part", "value")
	if restored != material || stderr != "" {
		t.Fatal("schema replacement changed existing Secret material")
	}
}
