# OpenShift agent install waits and host registration

What `openshift-install agent wait-for` waits on, when the installation starts,
and what stalls it. [Container clusters](../../specs/openshift-clusters.md) owns
the required behavior. This page records native facts for the M3 items that
size and bound an installation:
[B711](../../specs/milestones/delivered.md#b711),
[B712](../../specs/milestones/delivered.md#b712),
[B714](../../specs/milestones/m3.md#b714) and
[B260](../../specs/milestones/m3.md#b260).

Provenance: `R<n>:C<m>` is a claim id of the M3 research, `RV` marks a claim the
review re-verified, and "observed by the v1 implementation (main-v1)" marks a
fact to re-verify on this tree. Release labels name where a fact was read; they
are not supported or required versions (D163). The ids name the M3 definition's
research and review notes of 2026-10-20, which this repository does not track;
each statement here carries its fact, and an id only records where it was read.
Assisted-service, assisted-installer and assisted-installer-agent behavior was
read at master, not per release.

## The wait windows

- `wait-for bootstrap-complete` has a compiled 60-minute window, polling every
  2 s. `install-complete` repeats it, then waits for ClusterVersion (40
  minutes) and 30 minutes of operator stability. The installer has no timeout
  flags (R1:C43).
- A 60-minute extension of the ClusterVersion wait on the baremetal platform is
  unconfirmed in the agent flow (RV R1:C43, uncertain), so budget 40.
- A bootstrap that does not finish ends with `Bootstrap failed to complete: :
  bootstrap process timed out: context deadline exceeded` and exit code 5. A
  host validation that never passes shows as repeated warnings followed by the
  60-minute timeout, not as an early failure (R1:C47).
- Exit codes: 3 install-config, 4 infrastructure, 5 bootstrap, 6 install, 7
  operator stability, 8 interrupt (R1:C44).
- Both waits are stateless and re-runnable against the original asset
  directory. `install-complete` after a finished bootstrap returns through the
  bootstrap ConfigMap check (R1:C45). This answers an open question the v1
  implementation recorded (B-041 of its untracked review notes): the waits can
  be repeated after an interruption.
- Host and cluster problems are logged as warnings. Only `cancelled` and a
  cluster ready for more than a minute abort early. `error` and
  `installing-pending-user-action` log once and the wait continues to the
  timeout (R1:C47).
- `Cannot access Rendezvous Host` appears after 2 minutes without either API
  (R1:C46).

## When the installation starts

- The installation starts only when the number of `known` hosts equals masters
  plus arbiters plus workers exactly, with a `!=` comparison, checked every 10 s
  (R1:C36, RV). More booted hosts than replicas never match, and one node that
  never registers keeps the wait at `Waiting for all hosts to be ready: N hosts
  expected`.
- An omitted `compute` section means three expected workers (R1:C21).
- The documentation names no host maximum. The assisted installer's per-stage
  host timers are writing image 30 minutes, rebooting 40, configuring 60,
  joined 60 and waiting for the control plane 60 (R1:C38, assisted-service
  master, medium confidence).
- Workers that never join leave the ingress unavailable and the cluster
  incomplete (R1:C38).

## Stranded hosts and contention

- A host in `error` stops the installation. Recovery excludes the rendezvous
  host's final pivot, so it keeps the bootstrap control plane and bootstrap can
  complete one node short. The usual trigger is a "Joined" stage past one hour
  from slow release-payload pulls (observed by the v1 implementation
  (main-v1); re-verify on this tree). [B711](../../specs/milestones/delivered.md#b711)
  owns the release rule.
- Two clusters installing at once on one controller converged slower and missed
  the installer's window (observed by the v1 implementation (main-v1);
  re-verify on this tree). [B712](../../specs/milestones/delivered.md#b712) owns the
  one-at-a-time rule.

## Certificate signing requests and tokens

- During installation the assisted-installer controller approves every pending
  CSR every 30 s until all hosts join. Nothing approves later CSRs (R1:C37, RV).
- The install-flow agent ISO's tokens do not expire. The day-2
  `oc adm node-image` flow mints ES256 tokens valid 48 hours, kept in Secret
  `openshift-config/agent-auth-token` and refreshed inside the last 24 hours,
  which invalidates earlier node images (R1:C40, RV).
- v1 bounded install-image reuse to 24 hours without a source. Whether any
  certificate the install image embeds expires is unverified; owner question
  [Q6](../../specs/milestones/m3.md#m3a-open-decisions) records the drill.

## Bounded assisted-host diagnosis

The existing inspection fixtures record the asset-state shape written by
`openshift-install` 4.21.10 (`6285755d`) during `agent create config-image`:
`*gencrypto.AuthConfig` carries `WatcherAuthToken` and
`*agentconfig.AgentConfig.Config` carries `rendezvousIP`. The existing
[work-area finding](openshift-agent-work-area.md) records what `agent create
image` writes into its directory; image-internal files are distinct from that
work directory.

The assisted-host fixtures are modeled response shapes, **not a captured live
assisted-service response**. They model a one-element cluster array with a
`hosts` array. A host has `requested_hostname`, JSON text in `inventory` whose
`hostname` is another name, `status`, `status_info`, and JSON text in
`validations_info`. The last maps validation categories to lists with `id` and
`status`; `failure` selects the identifiers reported in a diagnosis. Empty or
malformed validation details leave the host's status visible. These shapes
are exercised by the
[inspection tests](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/modules/test_containercluster_install_inspect.py)
and [wait tests](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_containercluster_budgets.py).

X118 extends that read to failed bootstrap timeouts and spent budgets, retaining
one bounded request and the original diagnosis when the read fails. Its tests
prove missing names precede bad-state hosts and that successful waits,
installation waits and unrelated failures make no assisted-service request.
The captured real host response required by B260, and verification of the
image's `rendezvous-host.env`, `common.sh`, `set-hostname.sh` and `agent.service`
against that installation, remain operator evidence to collect; these modeled
fixtures do not satisfy that provenance gate.
