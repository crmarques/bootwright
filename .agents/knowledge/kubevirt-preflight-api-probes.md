# KubeVirt Preflight API Probes

KubeVirt host-cluster preflight probes address exact Kubernetes REST resource
paths through `kubectl get --raw`. Typed `kubectl get` commands initialize API
discovery before reading the target resource; when an endpoint or load balancer
returns a generic HTTP 404, client-go emits repeated `memcache.go` discovery
errors that obscure the actual failure.

A `NotFound` is evidence that OpenShift Virtualization or a network attachment
is absent only when the response names the exact requested resource. A generic
`the server could not find the requested resource` response means the
kubeconfig does not currently reach the expected Kubernetes API surface. Once
the host API prerequisite fails, dependent network probes must not run.
