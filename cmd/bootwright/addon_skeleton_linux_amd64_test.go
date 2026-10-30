//go:build linux && amd64

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/catalog"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

func TestEveryPackageSkeletonCompilesOnceItsPlaceholdersAreReplaced(t *testing.T) {
	bases := map[string]struct{ example, cluster, consumer string }{
		"nmstate":  {"lab-sno-nmstate", "sno", ""},
		"kubevirt": {"lab-sno-nmstate", "sno", ""},
		"metallb":  {"lab-sno-nmstate", "sno", ""},
		"acm":      {"lab-compact-hub", "compact", ""},
		"gitops":   {"lab-compact-hub", "compact", ""},
		"fdf":      {"lab-compact-df", "compact", "compact-storage"},
	}
	service := catalog.New(packages.Builtin())
	listing, err := service.List(context.Background(), catalog.ListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.AddOns) != len(bases) {
		t.Fatalf("the test configures %d packages, the registry holds %d", len(bases), len(listing.AddOns))
	}
	for _, entry := range listing.AddOns {
		t.Run(entry.Name, func(t *testing.T) {
			base, ok := bases[entry.Name]
			if !ok {
				t.Fatalf("package %s has no test configuration", entry.Name)
			}
			result, err := service.Describe(context.Background(), catalog.DescribeRequest{Name: entry.Name})
			if err != nil {
				t.Fatal(err)
			}
			body := result.Package.Binding
			for placeholder, value := range map[string]string{"<clusterRef>": base.cluster, "<channel>": "example-channel", "<startingCSV>": "example-operator.v0.0.0", "<source>": "redhat-operators", "<cephConsumerRef>": base.consumer} {
				body = strings.ReplaceAll(body, placeholder, value)
			}
			if strings.ContainsAny(body, "<>") {
				t.Fatalf("a placeholder has no test value:\n%s", body)
			}
			sources := exampleDirectory(t, base.example)
			replaced := 0
			for i, file := range sources.Files {
				if strings.Contains(string(file.Bytes()), "kind: ClusterAddonBinding\n") {
					sources.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(body))
					replaced++
				}
			}
			if replaced != 1 {
				t.Fatalf("%s holds %d bindings, want 1", base.example, replaced)
			}
			_, report := compileAcceptance(t, sources)
			if len(report.Advisories) != 0 {
				t.Fatalf("advisories %+v for\n%s", report.Advisories, body)
			}
		})
	}
}
