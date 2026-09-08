---
name: security-safety
description: Review or implement Bootwright trust and effect boundaries, including secrets, untrusted input, filesystem and process I/O, remote access, privilege, mutation, and supply-chain trust.
---

# Security And Safety

Use [code-implementation](../code-implementation/SKILL.md) for tracked edits.
[Security](../../../specs/security.md) owns trust and effect requirements;
[state reconciliation](../../../specs/state-reconciliation.md) owns mutation
proofs and recovery. This skill guides review without granting effect authority.

- For the affected operation, identify protected data, untrusted inputs and
  outputs, exact target and identity, authority, permitted effects, resource
  bounds, and failure/cancellation states. Resolve missing mutation boundaries
  before implementing effects; confirmation cannot supply missing proof.
- Trace sensitive data through every consumer, including tool output, error
  wrapping, logs, evidence, temporary artifacts, and cleanup. Verify redaction
  before formatting or persistence, and inspect failure paths as well as normal
  output. Use synthetic fixtures and examples.
- Review paths at the actual open/publish boundary, process arguments and
  environment at invocation, and endpoints and trust at connection. Test
  substitution and ambient-authority injection; validation at an earlier layer
  alone does not prove containment at the effect edge.
- Treat an unsuccessful or ambiguous probe as unknown. For mutation, trace
  identity, ownership/absence, authorization, and time-sensitive revalidation
  into the selected operation. Define the interruption and recovery evidence
  before implementing cleanup or retries.
- Check dependency use against the operation's effect and integrity boundaries.
  For frozen recovery, apply the state contract without silently substituting
  a dependency.
- Load [Go](../code-implementation/references/go.md) for Go trust edges and
  [Ansible](../code-implementation/references/ansible.md) for managed remote
  operations. For bare-metal install, root-disk selection, or erase, also read
  [the disk-safety finding](../../knowledge/openshift-agent-disk-safety.md).

Prove both the intended effect and absence of prohibited writes, network
calls, processes, secret reads, and disclosures. Include applicable traversal,
substitution, bounds, unknown probes, wrong identity, interruption, and cleanup
failures. Report findings with the violated contract, evidence, impact,
smallest correction, and verification. Distinguish a missing test from a
proven safety failure and never report unavailable evidence as a pass.
