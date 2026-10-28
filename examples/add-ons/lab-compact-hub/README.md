# ACM and GitOps on compact OpenShift

[lab-compact](../../openshift-clusters/lab-compact/) topology plus one
`ClusterAddonBinding` installing Advanced Cluster Management (ACM) with a
pinned multicluster engine, then OpenShift GitOps last, with a bootstrap Argo
CD root Application. This is the
[B766 hub rehearsal](../../../specs/milestones/m4.md#b766); operator acceptance
remains open and requires the accepted compact-cluster baseline.

## Worth noting

- Three schedulable masters `compact-01` to `compact-03`, each 8 vCPU, 24 GiB
  memory and a 120 GiB root disk. Host capacity: at least 28 logical CPUs,
  80 GiB available memory and 400 GiB free pool disk, plus about 7 GiB for five
  image copies; the provider reserves 4 CPUs, 8 GiB and 40 GiB of it. See the
  [acceptance profiles](../../../specs/openshift-coverage.md#acceptance-profiles).
- The channels and CSVs in [add-ons/binding.yaml](add-ons/binding.yaml) (ACM
  `release-2.16`, engine `stable-2.11`, GitOps `gitops-1.18`) illustrate the
  shape. Choose versions your catalog serves and that suit the selected
  OpenShift release before initializing the context.
- The bootstrap repository `https://git.example.test/platform/bootstrap.git`
  is synthetic: replace it with one you control. The `bootstrap-repository`
  Secret supplies its username and password or token, bound from the operator;
  the declaration holds no credential bytes.
- For the permission drill, set `bootstrap.access.clusterRole` to a dedicated
  ClusterRole granting only what the test repository needs. The repository must
  exclude every Bootwright-owned object, including the root Application and
  repository Secret. Bootwright creates the root once and never waits for
  synchronization.
- All addresses and names are synthetic; the topology substitutions are
  lab-compact's. Remaining version, permission and credential checks are
  recorded in B766.

Runbook: [e2e/runbooks/lab-compact-hub.md](../../../e2e/runbooks/lab-compact-hub.md).
