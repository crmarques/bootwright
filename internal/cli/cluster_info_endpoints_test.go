package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/environment/inspection"
)

func renderedClusterInfo(t *testing.T, detail string) []string {
	t.Helper()
	var out bytes.Buffer
	if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "text", Width: 220, Detail: detail}), io.Discard, "cluster info", storageOnly(readyCeph()), false); err != nil {
		t.Fatal(err)
	}
	return strings.Split(out.String(), "\n")
}

func hasFields(lines []string, want ...string) bool {
	return slices.ContainsFunc(lines, func(line string) bool { return slices.Equal(strings.Fields(line), want) })
}

func TestClusterInfoEndpointsCompactAndFull(t *testing.T) {
	compact := renderedClusterInfo(t, "compact")
	if !hasFields(compact, "Endpoint", "mgmt-gateway", "ready") || !hasFields(compact, "Endpoint", "nfs.files", "ready") ||
		!hasFields(compact, "Endpoint", "rgw.objects", "unproved:", "the", "apply", "did", "not", "prove", "this", "endpoint", "reachable") {
		t.Fatalf("compact endpoints missing:\n%s", strings.Join(compact, "\n"))
	}
	for _, line := range compact {
		if strings.Contains(line, "9443") || strings.Contains(line, "nfs://") || strings.Contains(line, "CD:0E") || strings.Contains(line, "objects.lab.test") {
			t.Fatalf("compact output carries endpoint detail: %q", line)
		}
	}
	full := renderedClusterInfo(t, "full")
	fingerprint := "CD" + strings.Repeat(":0E", 31)
	for _, want := range [][]string{
		{"Endpoint", "mgmt-gateway", "ready"}, {"https://192.0.2.50:9443/"}, {fingerprint, "(cephadm-signed;", "compare", "before", "accepting", "the", "browser", "warning)"},
		{"nfs://192.0.2.60:2049/"}, {"https://objects.lab.test:8443/"},
	} {
		if !hasFields(full, want...) {
			t.Fatalf("full endpoints miss %v:\n%s", want, strings.Join(full, "\n"))
		}
	}
	if strings.Contains(strings.Join(full, "\n"), "bound certificate") {
		t.Fatal("an unproved bound endpoint reported a certificate")
	}
}

func TestClusterInfoEndpointRowsMustBeConsistent(t *testing.T) {
	if !validClusterInfo(storageOnly(readyCeph())) {
		t.Fatal("the ready fixture is not valid")
	}
	digest := "cd" + strings.Repeat("0e", 31)
	for name, edit := range map[string]func(*inspection.ManagedCeph){
		"ready with a reason":         func(c *inspection.ManagedCeph) { c.Endpoints[1].Reason = "because" },
		"unproved without a reason":   func(c *inspection.ManagedCeph) { c.Endpoints[2].Reason = "" },
		"a digest on an unproved row": func(c *inspection.ManagedCeph) { c.Endpoints[2].CertificateSHA256 = digest },
		"a digest without an issuer": func(c *inspection.ManagedCeph) {
			c.Endpoints[1].CertificateSHA256 = digest
		},
		"a malformed digest":             func(c *inspection.ManagedCeph) { c.Endpoints[0].CertificateSHA256 = "cd" },
		"http with an issuer":            func(c *inspection.ManagedCeph) { c.Endpoints[2].URLs = []string{"http://objects.lab.test:80/"} },
		"https without an issuer":        func(c *inspection.ManagedCeph) { c.Endpoints[0].Issuer = "" },
		"an unknown issuer":              func(c *inspection.ManagedCeph) { c.Endpoints[0].Issuer = "other" },
		"an unknown status":              func(c *inspection.ManagedCeph) { c.Endpoints[1].Status = "done" },
		"an unknown scheme":              func(c *inspection.ManagedCeph) { c.Endpoints[1].URLs = []string{"ftp://192.0.2.60/"} },
		"no urls":                        func(c *inspection.ManagedCeph) { c.Endpoints[1].URLs = nil },
		"an empty service":               func(c *inspection.ManagedCeph) { c.Endpoints[1].Service = "" },
		"unsorted services":              func(c *inspection.ManagedCeph) { c.Endpoints[0], c.Endpoints[1] = c.Endpoints[1], c.Endpoints[0] },
		"duplicate services":             func(c *inspection.ManagedCeph) { c.Endpoints[1].Service = c.Endpoints[0].Service },
		"an unavailable entry with rows": func(c *inspection.ManagedCeph) { *c = *unavailableCeph(); c.Endpoints = readyCeph().Endpoints },
	} {
		ceph := readyCeph()
		edit(ceph)
		if validClusterInfo(storageOnly(ceph)) {
			t.Errorf("%s was accepted", name)
		}
	}
	if !validClusterInfo(storageOnly(unavailableCeph())) {
		t.Fatal("an unavailable entry without endpoints is not valid")
	}
}

func TestClusterInfoEndpointsEscapeEveryString(t *testing.T) {
	ceph := readyCeph()
	ceph.Endpoints[0].URLs = []string{"https://192.0.2.50:9443/\x1b[31m"}
	var out bytes.Buffer
	if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "json"}), io.Discard, "cluster info", storageOnly(ceph), true); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(out.Bytes(), &document); err != nil || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("an escape byte reached the document (%v): %q", err, out.String())
	}
}
