package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type mirrorLender struct {
	held      map[string]secrets.Material
	requested []string
}

func (l *mirrorLender) WithMaterial(ctx context.Context, request lifecycle.MaterialRequest, use func(context.Context, map[string]secrets.Material) error) error {
	l.requested = append(l.requested, request.Secrets...)
	lent := map[string]secrets.Material{}
	for _, name := range request.Secrets {
		if material, ok := l.held[name]; ok {
			lent[name] = material
		}
	}
	return use(ctx, lent)
}

func mirroredInput(trust string) api.Catalog {
	environment := api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef").WithPath(api.StringValue("https://mirror.example.test/tools"), "mirror", "artifacts", "url")
	if trust != "" {
		environment = environment.WithPath(api.StringValue(trust), "mirror", "artifacts", "trustBundleRef")
	}
	machine := api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").WithPath(api.MapValue(), "proxy", "direct")
	cluster := api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.21.15"), "distribution", "release", "version")
	return api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, environment),
		api.NewObject(api.Machine, "controller", api.Value{}, machine),
		api.NewObject(api.OpenShiftCluster, "hub", api.Value{}, cluster),
	})
}

// The probe reads only the mirror's public-origin clients, over the stage's
// own route, with the anchor Secret lent for the probe alone; a vendor client
// is never probed.
func TestThePreflightProbeReadsTheMirroredClientsWithTheLentAnchor(t *testing.T) {
	anchor := []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")
	lender := &mirrorLender{held: map[string]secrets.Material{"mirror-ca": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: anchor})}}
	var probed []bundlelocal.MirrorFile
	var lent []byte
	probe := mirrorProbe{material: lender, read: func(_ context.Context, files []bundlelocal.MirrorFile, request clients.Request, given []byte) ([]bundlelocal.MirrorReading, error) {
		probed, lent = slices.Clone(files), bytes.Clone(given)
		if request.Machine != "controller" || request.Egress.NoProxy == nil {
			t.Errorf("the probe does not take the stage's route: %+v", request)
		}
		readings := make([]bundlelocal.MirrorReading, 0, len(files))
		for _, file := range files {
			readings = append(readings, bundlelocal.MirrorReading{File: file, Status: 200, Served: file.Bytes})
		}
		return readings, nil
	}}
	files, err := probe.ProbeMirror(context.Background(), "lab", mirroredInput("mirror-ca"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(probed) != 1 || !strings.HasPrefix(files[0].URL, "https://mirror.example.test/tools/helm/v") || !strings.HasPrefix(files[0].Publisher, "https://get.helm.sh/helm-v") ||
		files[0].Bytes <= 0 || files[0].Served != files[0].Bytes || files[0].Status != 200 || probed[0].URL != files[0].URL {
		t.Fatalf("files %+v, probed %+v", files, probed)
	}
	if !bytes.Equal(lent, anchor) || !slices.Equal(lender.requested, []string{"mirror-ca"}) {
		t.Fatalf("the probe was lent %q after asking for %v", lent, lender.requested)
	}
	lender.requested = nil
	if _, err := probe.ProbeMirror(context.Background(), "lab", mirroredInput("")); err != nil || len(lender.requested) != 0 || lent != nil {
		t.Fatalf("a mirror without an anchor read %v and lent %q (%v)", lender.requested, lent, err)
	}
	_, err = probe.ProbeMirror(context.Background(), "lab", mirroredInput("missing-ca"))
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "secret.part" || !strings.Contains(found[0].Message, "Secret missing-ca") {
		t.Fatalf("a missing anchor = %v", err)
	}
}
