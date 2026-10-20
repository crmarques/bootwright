# GitLab Operator on OpenShift

How GitLab installs through its operator on OpenShift, what it needs around it,
when it is ready, and what removing it keeps. The package is
[B93](../../specs/milestones/m4.md#b93), its certificate provider
[B767](../../specs/milestones/m4.md#b767), the live proof
[B768](../../specs/milestones/m4.md#b768), and GitLab's supported route, the
operator's release manifest, is the first candidate of
[B786](../../specs/milestones/backlog.md#b786). The OLM mechanics are in
[olm-v0-exact-install.md](olm-v0-exact-install.md). B93's operator-run OLM
probe, before its slice, records here which webhook certificate the
controller serves under OLM and whether a minimal `GitLab` create is
admitted.

Provenance: `R5:C<n>` is a claim id of the M4 definition's research of
2026-10-20 (GitLab on OpenShift), `S<n>` a summary item and `Q<n>` an open
question; `RV-R5` is its adversarial review, with the verdict (confirmed,
corrected or uncertain) and its corrections 1 to 5 as the governing text where
they differ. Release labels name where a fact was read; they are not supported
or required versions (D163), and the requirement figures below are GitLab's
statements, not Bootwright rules. The ids name research notes this repository
does not track; each statement here carries its fact, and an id only records
where it was read.

## What GitLab says about the route

- GitLab's operator documents "known limitations" and is "only suitable for
  specific scenarios in production use", with no general-availability or beta
  label. The installation page also says that on OpenShift installation "is
  typically handled by the Operator Lifecycle Manager" and then warns that
  "Installation using OLM is considered experimental. GitLab does not support any
  issues related to instances deployed using OLM". The same text is in the CSV
  description and the CSV carries `maturity: alpha` (S1; R5:C2 to C4,
  confirmed).
- The recommended routes are the release manifest or the operator's Helm chart.
  The manifest comes in four files (`kubernetes`, `openshift` and the two
  `namespaced` forms) and defaults to the namespace `gitlab-system`. The Helm
  route is an OCI chart. The OLM tab names no package or channel (R5:C1,
  confirmed). ADR 21 lists the conditions for lifting the label: a documented
  upgrade path under OLM, automated OLM releases and resolution of issue 241
  (R5:C4, confirmed).
- Known limitations: no management of an existing Helm-chart instance, no Git over
  SSH through OpenShift Routes, no workload identity, and zero-downtime upgrades
  one minor at a time (R5:C2). The operator must be installed by a cluster
  administrator (R5:C15). It does not include GitLab Runner; a separate
  `gitlab-runner-operator` package exists in the same catalogs (R5:C38).
- The operator's OpenShift support table marks minors Supported, Deprecated or
  Unsupported and has no row for a pre-release minor (R5:C9, confirmed). It is
  evidence, not a rule.

## The package

- `gitlab-operator-kubernetes` exists in `certified-operators` and in
  `community-operators`, channels `stable` (default) and `unstable`, with the
  same bundle in both. The certified and community CSVs differ only in
  `createdAt`, and the bundle annotations give a catalog range up to 4.22. A
  cluster with both default sources would see the package twice (an inference,
  R5:C5 medium), so a Subscription names `source` and a PackageManifest query
  filters by catalog (R5:C5 and S12; RV-R1 C56, confirmed).
- The CSV supports OwnNamespace only, so the `GitLab` resource lives in the
  operator's namespace and the OperatorGroup targets itself. It suggests
  `gitlab-system`, runs the Deployment `gitlab-controller-manager`, declares
  `features.operators.openshift.io/disconnected: "false"`, `fips-compliant`,
  `proxy-aware` and `tls-profiles` all `"false"`, and replaces the previous CSV in
  a linear chain (R5:C6 and C7, confirmed).
- The CSV carries `olm.maxOpenShiftVersion` `4.22`. Classic OLM honours it: Red
  Hat reports show the OLM ClusterOperator going `Upgradeable=False` with
  reason `IncompatibleOperatorsInstalled` (the ClusterOperator's name is not
  settled by the research; the gate records it from `oc get clusteroperator`),
  so an installed GitLab CSV blocks the
  next minor update until GitLab publishes a bundle without it. Whether the
  next-minor catalogs will carry the bundle is uncertain (RV-R5 correction 4;
  C8 corrected, Q4). See
  [olm-v0-exact-install.md](olm-v0-exact-install.md).
- Each operator release lists exactly three chart versions, and the operator
  rejects any other with `Configuration error detected: chart version <v> not
  supported; please use one of the following: ...`. The charts are copied into the
  operator image, so there is no chart download at run time. Chart 10.N.x matches
  GitLab 19.N.x (R5:C10, C32 and C51, confirmed).
- Only `apps.gitlab.com/v1beta1` ships. The successor resources are in source and
  in no release. ADR 42 calls the v1beta1 controller "otherwise frozen" and its
  migration refuses an instance that uses the bundled NGINX or cert-manager
  certificates for Ingresses, so a Gateway API or Routes configuration keeps that
  migration open (RV-R5 correction 5; R5:C14, confirmed).

## What it needs around it

- GitLab requires external PostgreSQL, Redis and object storage. Chart 10.0
  removed the bundled PostgreSQL, Redis and MinIO, so no bundled option exists on
  a 10.x chart; an operator release that still allows a 9.x chart can deploy them
  (R5:C16 and C40, S4, confirmed).
- PostgreSQL, as GitLab states it for 19.x: a minimum of 17 and a maximum of 18;
  the extensions `amcheck`, `btree_gist`, `pg_trgm` and `plpgsql`, which need a
  superuser or loading beforehand; an empty `gitlabhq_production` database and a
  user with full access. Chart keys: `global.psql.host`, `.password.secret`,
  `.password.key`, optional `port`, `database`, `username` and `ssl.*`
  (R5:C41, confirmed; the chart keys were not re-read).
- Redis 7.0 minimum and 7.2 recommended, or Valkey 7.2. Standalone only: Redis
  Cluster is unsupported and Sentinel is supported. Chart keys: `global.redis.host`,
  `.auth.enabled`, `.auth.secret`, `.auth.key`, optional `port`, `database` and
  `scheme: rediss` (R5:C42, confirmed).
- Object storage needs a bucket for each type: CI artifacts, LFS, packages,
  uploads, external merge request diffs, Terraform state, CI secure files and the
  dependency proxy, plus the registry, backups with a temporary bucket and Pages.
  "A different bucket is needed for each, otherwise performing a restore from
  backup doesn't function properly." The consolidated form is one connection
  Secret whose key `connection` holds a Fog YAML document (`provider: AWS`,
  region, access keys and, for S3-compatible stores, `aws_signature_version: 4`,
  `endpoint` and `path_style: true`). The registry takes a separate Secret under
  `config` with the distribution storage driver block, and the bucket named in
  both that Secret and `global.registry.bucket`. Backups use a third Secret in
  `.s3cfg` form (R5:C43, confirmed).
- GitLab tests only Amazon S3, Google Cloud Storage and Azure Blob. Ceph RGW is
  under community-documented providers: `provider: AWS`, `endpoint`, `region`,
  access keys and `path_style: true`, with server-side encryption only over
  HTTPS. GitLab says claimed S3 compatibility "does not guarantee a provider
  works" and lists `enable_signature_v4_streaming: false` and
  `aws_signature_version: 2` as knobs. IBM Storage Ceph is not mentioned, and
  whether it works is open (R5:C44 and S6, confirmed; Q7).
- For production GitLab says PostgreSQL and Redis run outside the cluster, on
  platform services or compute, and calls the reference architecture "Cloud
  Native Hybrid". CloudNativePG is named as an acceptable PostgreSQL, and the
  operator's own end-to-end fixtures use CloudNativePG, Valkey and Garage
  (R5:C39, confirmed).
- Resource guidance: the chart "is intended to fit in a cluster with at least 8
  vCPU and 30 GB of RAM"; the operator's end-to-end suite says a small instance
  needs about 8 GiB of memory on one node; the operator pod requests 200m CPU and
  250Mi (R5:C6, not re-read by RV; R5:C29, confirmed for the rest). Gitaly's PVC defaults to 50Gi, `ReadWriteOnce`, on
  the default StorageClass, named `repo-data-<release>-gitaly-<n>`; Gitaly on
  Kubernetes is generally available with requirements written for containerd
  cgroups, and CRI-O is not discussed (R5:C27 and C28, confirmed; Q6).

## cert-manager and the webhook certificate

- "To create a certificate for the operator's Kubernetes webhook, cert-manager is
  used", and "you can't use the cert-manager bundled with the GitLab Chart.
  Instead, install cert-manager before you install the operator". No version is
  given. The operator's override forces `installCertmanager: false` and
  `gitlab-runner.install: false` (R5:C17, S7, confirmed).
- The release manifest creates a `cert-manager.io/v1` self-signed Issuer and
  Certificate, and the webhook configuration carries `cert-manager.io/inject-ca-from`,
  which needs a cainjector. The Red Hat cert-manager Operator runs a cainjector
  with its operator in `cert-manager-operator` and its operands in `cert-manager`,
  both namespaces hard-coded, so it provides the API group and controllers; GitLab
  never names or tests it (R5:C17, medium).
- The OLM bundle has eight files and ships no Certificate, Issuer or SCC. The CSV
  declares a webhook definition (`vgitlab.kb.io`, failure policy `Fail`, target
  port 9443) and a Deployment volume `cert` from the Secret `webhook-server-cert`,
  which the manifest install creates from the Certificate. GitLab documents the
  symptom `MountVolume.SetUp failed for volume "cert" : secret
  "webhook-server-cert" not found` and says the operator depends on cert-manager.
  Whether OLM's own webhook certificate replaces or collides with that volume is
  undetermined (R5:C18, confirmed for the files; Q2). The CSV's cluster
  permissions name `gitlab-nginx-ingress-scc` although no SCC ships.
- The webhook `gitlab-validating-webhook-configuration` has `failurePolicy: Fail`
  on create and update of `gitlabs`, served by `gitlab-webhook-service` on port
  443 to 9443. If the operator pod or its certificate is not ready, the apply of
  the `GitLab` is refused (R5:C36, confirmed).
- The operator defaults `global.ingress.configureCertmanager` to true, the
  opposite of the chart default, and adds issuer annotations to Ingresses.
  Setting only that flag false is not enough on chart 10: the Gateway API Issuer
  defaults on, and on a cluster with no cert-manager the Job that applies it
  fails. On charts before 10.5, which operator 3.5.0 still allows,
  `global.gatewayApi.enabled=false` does not deactivate that Issuer, so
  `global.gatewayApi.configureCertmanager: false` must be set explicitly when own
  certificates are supplied (RV-R5 correction 3; C23 corrected). The chart's
  Issuer is Let's Encrypt HTTP01, which needs public reachability and public and
  private DNS parity, and its email falls back to `admin@example.com` when unset,
  an untested inference (R5:C23 and C54). A lab cluster without public ACME
  therefore sees the issuer Job fail.

## Routing

- Three modes are documented. Gateway API with Envoy Gateway is recommended
  (status Beta) and supports Git over SSH through a `TCPRoute`; the operator
  bundles no Gateway API controller. Bundled NGINX Ingress (deprecated in
  GitLab 19.0, chart 10.0, with removal in GitLab 20.0) needs a LoadBalancer Service and, on OpenShift, a workaround for the
  router rewriting the Ingress address. OpenShift Routes need no extra component
  and cannot carry Git over SSH (R5:C19 and C26, confirmed).
- Chart 10.x flipped the defaults: `nginx-ingress.enabled: false`,
  `global.ingress.enabled: false`, `global.gatewayApi.enabled: true`,
  `installEnvoy: true`, `prometheus.install: true`, `global.edition: ee`. The
  operator's minimal example CR was not updated, and the CSV's `alm-examples` sets
  its own values (`global.ingress.enabled: true`, `nginx-ingress.enabled: true`,
  `global.gatewayApi.enabled: false`). What a minimal CR renders on OpenShift is
  unconfirmed (R5:C20, confirmed as text; Q1).
- The operator does set Gateway values: `default-values.tpl` sets
  `gatewayApiResources.class.controllerName` to Envoy Gateway's controller, and
  with the legacy configuration also `global.gatewayApi.class.controllerName`. A
  Gateway API mode on another controller overrides the class or controller name,
  or uses `global.gatewayApi.gatewayRef`, and sets `installEnvoy: false`. The
  operator's own end-to-end suite uses Traefik (RV-R5 correction 2; C20
  corrected).
- The Routes recipe: `nginx-ingress.enabled: false`; `global.hosts.domain` set to
  the cluster apps domain (the host of the `console` Route in `openshift-console`
  without its prefix); `global.ingress.class: none` so the OpenShift router takes
  ownership; and the annotation `route.openshift.io/termination: "edge"` for TLS.
  The recipe predates the chart 10 defaults and does not set
  `global.ingress.enabled` (R5:C21, confirmed; medium that it works unchanged).
- Host names come from `global.hosts`: `gitlab.<domain>`, `registry.<domain>`,
  `kas.<domain>`, `pages.<domain>` and `smartcard.<domain>`, each overridable. For
  NGINX the documents say to create individual A records rather than a wildcard so
  existing Routes keep working (R5:C22, uncertain: the DNS quotes were not
  re-located).
- Security context constraints: the operator Deployment and pods it creates run
  under `restricted`; application pods use `nonroot-v2` through the ServiceAccount
  `gitlab-app-nonroot`; bundled NGINX uses the custom SCC
  `gitlab-nginx-ingress-scc`, which only the release manifest ships. The
  troubleshooting page says to extract it and apply it if NGINX pods are rejected.
  The operator terminates pods not running as the expected UID (R5:C24, confirmed;
  Q5).
- On OpenShift set `prometheus.install=false` in the `GitLab` values; OpenShift
  ships a Prometheus adapter. The chart default is `true` (R5:C25, confirmed).

## The GitLab resource

- `apps.gitlab.com/v1beta1` `GitLab`, short name `gl`, namespaced. `spec.chart.version`
  is a semver string and `spec.chart.values` is free-form
  (`x-kubernetes-preserve-unknown-fields`), so the schema validates little. Status
  has `phase`, `version` and standard conditions (R5:C30, confirmed).
- `metadata.name` is the Helm release name and prefixes every object. The name
  `gitlab` in `gitlab-system` keeps derived names predictable, and another name
  needs `global.ingress.class: gitlab-nginx` for the NGINX IngressClass (R5:C31,
  confirmed). The chart edition is `ee` by default; `ce` through the operator is
  inferred from pass-through and unverified, and the certified image table lists
  only `-ee` images (R5:C33, medium; Q11, tracked as
  [B790](../../specs/milestones/backlog.md#b790)).
- The root password Secret is `<name>-gitlab-initial-root-password`, key
  `password`. If absent, the shared-secrets Job generates a random password. To
  supply one, create the Secret before the `GitLab` (at least 6 characters). It
  applies only on first initialization (R5:C34, confirmed; the last part is
  medium).
- Phases are `Preparing` and `Running`; conditions are `Initialized`, `Upgrading`
  and `Available`. `Available=True` ("GitLab is running and available to accept
  requests") holds only when every Webservice and Sidekiq Deployment is complete,
  and the reconcile requeues every 10 seconds otherwise. It says nothing about
  Gitaly, the registry, KAS, the migrations Job or Route reachability. The install
  page's sample shows `Ready` and the upgrade page `Running`; the code constant is
  `Running` (R5:C35, confirmed; Q10).
- Chart documents say a deployment "may take 5-10 minutes", and the operator's
  end-to-end suite allows 10 minutes for a rollout. No install-to-ready duration
  on OpenShift was published (R5:C49, medium; Q8).
- Upgrade: back up, apply the new operator, wait for its pod to lead, then raise
  `spec.chart.version` to one of the three allowed versions. Zero-downtime mode
  moves one minor at a time; the annotation `gitlab.io/disable-zero-downtime-upgrade:
  "true"` scales Webservice and Sidekiq to zero, migrates and restores, and should
  be removed afterwards. Under OLM GitLab suggests `Manual` approval and
  `startingCSV`, and OLM cannot downgrade (R5:C37, confirmed).

## Removal

- The `GitLab` has no finalizer, so deleting it is garbage collection of the
  objects it owns. The user document's "all associated objects except for
  Persistent Volume Claims" overstates PVC survival (RV-R5 correction 1; S10 and
  C46 corrected).
- What survives: the Gitaly StatefulSet-template PVCs and the Secrets the
  shared-secrets Job created. What is deleted with the resource: the Prometheus
  history and Toolbox scratch PVCs, Gateways and EnvoyProxies, LoadBalancer
  Services (releasing the address) and the cert-manager Issuers the controller
  created. A new `GitLab` of the same name and namespace reuses the Gitaly data,
  the Rails secret and the root-password Secret. Disabling or renaming a
  component through values prunes nothing.
- A GitOps tool recreates a deleted `GitLab` unless it is removed from the
  repository first.
- After a Helm uninstall with bundled Envoy Gateway, an orphaned GatewayClass with
  finalizers and Envoy secrets can remain; the operator-render equivalent is not
  documented (R5:C47, medium).
- Deleting the operator manifest keeps the namespace, and removes the CRD and
  SCC, so a live `GitLab` would be removed with it; the CRD cascade is an inference
  (R5:C48, medium; Q13). Under OLM the steps are deleting the Subscription and the
  CSV, with CRD handling unverified.

## Disconnected

- The charts are inside the operator image and the operator needs only its own
  image and the operand images. The CSV's `relatedImages` lists only the operator
  image (twice), so a catalog-driven mirror misses the operand images the
  operator renders (R5:C51 and C53, S12, confirmed).
- The certified image table lists the operand images under
  `registry.gitlab.com/gitlab-org/build/cng/` tagged `$GITLAB_VERSION-ubi`
  (Gitaly, the registry, KAS, Shell, Sidekiq, Toolbox, Webservice, Workhorse and
  helpers) and the NGINX controller. The same page's prose shows a stale `-ubi8`
  tag; the images are cosign-signed with a stated identity (R5:C50, confirmed).
- There is no global registry override. `global.image.pullPolicy`, `.pullSecrets`
  and `.tagSuffix` exist, `global.image.registry` works only for Mailroom, and
  each subchart otherwise takes its own `image.repository`.
  `global.gitlabVersion` changes the default tag of Webservice, Sidekiq and
  migrations only. Images are mirrored per subchart, or by tag at the cluster
  level (R5:C52, confirmed).
- Let's Encrypt, the e2e fixtures' images and a Gateway API controller with its
  CRDs are further external dependencies an air-gapped install cannot satisfy by
  default (R5:C54, medium).

## Open live questions

R5's Q1 to Q14 each have a command in the research notes.
[B768](../../specs/milestones/m4.md#b768) re-verifies and records: what the
rendered values produce (Q1), which certificate the webhook serves under OLM
(Q2), Gitaly on CRI-O with cgroup v2 (Q6), uploads and a registry push against the
chosen S3 endpoint, including whether the RGW CA must be injected through
`global.certificates.customCAs` (Q7), the printed phase and condition generation
(Q10), the phase durations (Q8) and the `olm` operator's `Upgradeable` condition
(Q4, evidence only). Q3 (which catalog serves the package), Q5 (the NGINX SCC
after an OLM install), Q9 (whether OpenShift's Gateway API implementation serves
GitLab, including `TCPRoute`), Q11 (Community Edition), Q12 (install-time
validation of a bad chart version), Q13 (deleting the manifest with a live
`GitLab`) and Q14 (a pre-release OpenShift minor) stay open.
