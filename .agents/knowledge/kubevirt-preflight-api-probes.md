# KubeVirt Preflight API Probes

KubeVirt host-cluster preflight probes address exact REST paths through
`kubectl get --raw`. Typed `kubectl get` commands initialize API discovery
before reading the target resource; when an endpoint or load balancer returns a
generic HTTP 404, client-go emits repeated `memcache.go` discovery errors that
obscure the actual failure.

The readiness probe uses the same
`/apis/subresources.kubevirt.io/v1/version` endpoint as version-matched
`virtctl` provisioning. When it fails, a separate Kubernetes `/version` probe
distinguishes a reachable host without OpenShift Virtualization from a
kubeconfig that does not reach the expected Kubernetes API surface. Once the
KubeVirt API prerequisite fails, dependent network probes must not run.
