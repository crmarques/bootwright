# Examples

This directory is the versioned home for synthetic Bootwright desired-state
examples. Every tracked example must:

- use `apiVersion: bootwright.io/v1alpha1` on each Bootwright object;
- conform to [the desired-state API](../specs/api.md);
- contain no secret bytes, real-machine identity, private endpoint, private
  topology, credential-bearing URL, or other environment-specific value; and
- put co-located YAML payloads below the exact reserved `playbooks`, `roles`,
  `collections`, `manifests`, or `secrets` roots so desired-state discovery
  never reads them.

[`multidc-platform/`](multidc-platform/) is a synthetic, non-operational
multi-datacenter graph. It demonstrates a large resource and reference surface
using fictional identities, documentation address ranges, and deliberately
fake operational coordinates. Its private Secret payload root is ignored.

Validate the directory as a whole:

```text
bootwright validate -f examples/multidc-platform
```

[`lab-rhel/`](lab-rhel/) is the single-machine lab: the development machine is
the controller, hosts the managed proxy, DNS, NTP and artifact-server
containers of the `infra-components` stage, and provides the libvirt guest
whose RHEL Bootwright installs through an emulated Redfish BMC. It uses
documentation addresses and a synthetic domain, and its README walks the
journey that prepares the controller from it.

[`lab-sno/`](lab-sno/) is that same single-machine lab with one
single-node OpenShift cluster instead of an installed RHEL guest: the same
managed proxy, DNS, NTP and artifact-server set, one libvirt Machine the
substrate realizes and the agent installer supplies the operating system of,
and the cluster that installs onto it. Its README walks the journey.

## Local work in progress

`examples/wip/` is intentionally ignored. It is the place for complete
environment-specific translations used for operator review before a safe
synthetic example is extracted. Such local cases are not part of Git history
and must remain ignored because their values describe real environments.

A translation may expose required immutable input that the source never
carried, such as an image checksum or digest. Its local README must identify
that gap for operator input; it must not invent a plausible value or claim the
graph is validation-ready.

Verify the boundary before working with local examples:

```text
git check-ignore -v examples/wip/
git status --short --ignored examples/
```

A WIP graph must satisfy the field tables and cross-object rules in
`specs/api/`; the absence of a successful contract-conforming validation never
counts as success.
