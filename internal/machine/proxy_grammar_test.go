package machine_test

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/secrets"
)

const placementRouteYAML = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  domains:
    base: example.test
  controller:
    machineRef: controller
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: controller
spec:
  os: {provided: true}
  access: {local: true}
  capabilities: [container-runtime]
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: services
spec:
  os: {provided: true}
  capabilities: [container-runtime]
  network:
    addresses:
      - {name: ip, address: 192.0.2.10/24}
  proxy:
    proxyRef: egress
    noProxy: ["bad entry!"]
---
apiVersion: bootwright.io/v1alpha1
kind: Proxy
metadata:
  name: egress
spec:
  management: external
  connection:
    httpsProxy: http://proxy.example.test:3128
---
apiVersion: bootwright.io/v1alpha1
kind: DNSServer
metadata:
  name: resolver
spec:
  management: managed
  machineRef: services
  implementation: dnsmasq
  endpoints:
    - {name: ip, addressRef: ip}
`

func TestAPlacementMachineRouteRefusesWhatTheProxyGrammarRefuses(t *testing.T) {
	compiler := compilation.NewCompiler(yamlstream.Parser{}, nil,
		compilation.Rules{Normalize: environment.Normalize, Validate: environment.Validate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, Validate: secrets.Validate},
		compilation.Rules{Normalize: machine.Normalize, ValidateAuthored: machine.ValidateAuthored, Validate: machine.Validate},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, Validate: infrastructureservices.Validate},
	)
	sources := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(placementRouteYAML))}, Roots: []string{"/synthetic"}}
	_, _, err := compiler.Compile(context.Background(), sources)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Field != "$.spec.proxy.noProxy[0]" || reported[0].Object == nil ||
		reported[0].Object.Kind != "Machine" || reported[0].Object.Name != "services" {
		t.Fatalf("diagnostics = %+v", reported)
	}
}
