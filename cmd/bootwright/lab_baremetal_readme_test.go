//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func baremetalReadme(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "examples", "lab-baremetal", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// The README's fetch-failure command reads the journal of the unit the
// artifact server request names for the context the README creates, so an
// operator sees the access log rather than an empty journal of no unit.
func TestLabBaremetalReadmeReadsTheArtifactServerUnitsJournal(t *testing.T) {
	readme := baremetalReadme(t)
	created := regexp.MustCompile(`bootwright context init --name (\S+)`).FindStringSubmatch(readme)
	if created == nil {
		t.Fatal("the README creates no context")
	}
	input := baremetalInput(t, reconciliation.Apply)
	input.Context.Name = created[1]
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	var units []string
	for _, binding := range resolver.Bindings() {
		capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s plan: %v", binding.Kind, diagnostics.Of(err))
		}
		for _, block := range contribution.Definitions {
			if !strings.HasPrefix(block.ID, "artifact-server-") {
				continue
			}
			var request struct {
				Unit string `json:"unit"`
			}
			if err := json.Unmarshal(block.Request, &request); err != nil || request.Unit == "" {
				t.Fatalf("the artifact server request names no unit: %v", err)
			}
			units = append(units, request.Unit)
		}
	}
	if len(units) != 1 {
		t.Fatalf("artifact server units = %v", units)
	}
	named := regexp.MustCompile(`journalctl -u (\S+)`).FindAllStringSubmatch(readme, -1)
	if len(named) == 0 {
		t.Fatal("the README names no unit journal")
	}
	for _, match := range named {
		if !slices.Contains(units, match[1]) {
			t.Fatalf("the README reads the journal of %s, not of the planned unit %s", match[1], units[0])
		}
	}
}

// The README's RSA serving certificate is a leaf: a self-signed openssl req
// -x509 certificate is CA:TRUE under the default configuration, and the
// secret store and the artifact server refuse a CA as a serving certificate.
func TestLabBaremetalReadmeServingCertificateIsALeaf(t *testing.T) {
	var commands []string
	for _, line := range strings.Split(baremetalReadme(t), "\n") {
		if strings.HasPrefix(line, "openssl req ") {
			commands = append(commands, line)
		}
	}
	if len(commands) == 0 {
		t.Fatal("the README makes no serving certificate")
	}
	for _, command := range commands {
		for _, extension := range []string{
			"-addext basicConstraints=critical,CA:FALSE",
			"-addext keyUsage=critical,digitalSignature,keyEncipherment",
			"-addext extendedKeyUsage=serverAuth",
			"-newkey rsa:2048",
		} {
			if !strings.Contains(command, extension) {
				t.Fatalf("the README's certificate command lacks %q: %s", extension, command)
			}
		}
	}
}
