package main

import (
	"strings"
	"testing"
)

const remoteServiceHost = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: {name: remote-services}
spec:
  capabilities: [container-runtime]
  os: {provided: true}
  network:
    addresses: [{name: service, address: 192.0.2.20}]
  access:
    ssh:
      addressRef: service
      user: operator
      auth: {privateKeyRef: remote-services-key}
      knownHostsRef: remote-services-host-key
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: remote-services-key}
spec: {type: sshKeyPair}
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: remote-services-host-key}
spec: {type: opaque}
`

const remoteManagedProxy = "apiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: egress}\nspec:\n  management: managed\n  implementation: squid\n  machineRef: remote-services\n  endpoints: [{name: provisioning, addressRef: service}]\n"

func TestAServicePlacementHostMustConnectAsRoot(t *testing.T) {
	trustFailure(t, serviceSources(serviceEnvironment, remoteServiceHost, remoteManagedProxy), "api.invariant", "$.spec.access.ssh.user")
	root := strings.Replace(remoteServiceHost, "user: operator", "user: root", 1)
	compileAcceptance(t, serviceSources(serviceEnvironment, root, remoteManagedProxy))
}
