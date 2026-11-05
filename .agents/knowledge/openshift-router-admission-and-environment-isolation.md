# Router admission and environment isolation on OpenShift

On OpenShift 4.22.21 the ingress operator creates a NetworkPolicy named
`router-<IngressController name>` in `openshift-ingress` for every router,
beside `openshift-ingress-deny-all`. Its one ingress rule lists the `http`,
`https` and `metrics` ports and no `from`, so it admits any source. The
operator owns that name: a policy of the same name applied by hand is replaced
by the operator's. NetworkPolicies only add allowances, so no policy in
`openshift-ingress` can keep one namespace's pods away from a router.

Observed in [X302](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node)'s run of
[lab-sno-fips-controller](../../e2e/lab-sno-fips-controller/runbook.md): a
production pod read the staging app through the staging router (HTTP 200 with
its host), by the router's MetalLB address, its pod address and its cluster
IP alike. The case's `router-staging` and `router-production` policies had
been replaced, and a JSON patch of them found nothing to remove.

**Decision it informs.** Isolate an environment at its own namespace's egress.
Each environment namespace there carries an `Egress` policy that admits its
own pods, `openshift-dns`, its own router's pods (selected by
`ingresscontroller.operator.openshift.io/deployment-ingresscontroller`) and
`0.0.0.0/0` except the cluster, service and node networks and the other
environment's network. In that run the policy also stopped a pod reaching the
other router by its MetalLB address and by its NodePort on the node's address;
whether OVN-Kubernetes matches it before or after load balancing was not read.
Traffic on an
environment's own Localnet network is a secondary network, which no
NetworkPolicy governs, so pods still reach their own ingress, apps and host
containers there. The EgressFirewall that denied the other environment's
network did not stop a pod reaching that environment's MetalLB address
(`routingViaHost: true`); the reason was not read. The
case's policies are `workloads/<env>/egress-networkpolicy.yaml`, and its
runbook's matrix probes each leaked path.

Applies to the case's workloads, which Bootwright does not apply
([product scope](../../specs/project.md#product-scope)); re-read the operator's
policies on another minor.
