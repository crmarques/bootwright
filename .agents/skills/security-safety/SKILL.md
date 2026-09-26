---
name: security-safety
description: Review or implement Bootwright trust and effect boundaries, including secrets, untrusted input, filesystem and process I/O, remote access, privilege, mutation, and supply-chain trust.
---

# Security And Safety

Use [code-implementation](../code-implementation/SKILL.md) for tracked edits.
[Security](../../../specs/security.md) owns trust and effect requirements and
lists the [proof each invariant needs](../../../specs/security.md#required-security-proof);
[state reconciliation](../../../specs/state-reconciliation.md) owns mutation
proofs and recovery. This skill guides review without granting effect authority.

## Rules this repository has already broken once

Each is, or must become, an executable guard. Check every one a change touches.

- A destructive physical path proves the exact target (system identity, complete
  MAC set, powered off) immediately before it inserts media, and again inside
  the installer. A root disk is named by a device path; an empty or wwn-only
  selector refuses. Read [the disk-safety finding](../../knowledge/openshift-agent-disk-safety.md).
- Consumer roles dispatch on the frozen substrate and fail closed on an unknown
  arm, and a task named for a proof never mutates. The structural tests live in
  `ansible/collections/ansible_collections/bootwright/core/tests/unit/test_role_task_order.py`.
- No secret or capability-bearing value (a token, a private URL, a key) enters a
  publicly served artifact. Private material travels only over verified TLS.
- Concurrent blocks share no mutable file: roles use the runner's per-invocation
  scratch, never a fixed host path.
- A durable write stages and renames, and removes its own stage when it fails; no
  refusal leaves state that blocks the next command.
- A decision taken under a shared lock is re-proved under the exclusive lock
  before it mutates.
- An operator-supplied file is opened once, without following links, and passed
  on as a descriptor; SSH configuration and ambient environment are isolated.

## Review

Trace sensitive data through outputs, logs, errors, evidence, temporary files
and cleanup. Check paths at the open boundary, arguments and environment at
invocation, and endpoints and trust at connection. Treat an unsuccessful or
ambiguous probe as unknown; confirmation never supplies missing proof. Prove the
intended effect and the absence of prohibited writes, network calls, processes,
secret reads and disclosures. Report the violated contract, evidence, impact,
smallest correction and verification. A missing test is not a proven failure,
and unavailable evidence is never a pass.
