# ADR 0016: Secret as a First-Class Kind

## Status

Accepted

## Context

Secret material — pull secrets, BMC credentials, RHSM keys, TLS certificate and
key pairs, SSH key pairs, CA bundles — was originally attached to the
`Environment` object under a `spec.secrets` block. That coupling had three
problems. Secret bytes must never enter versioned input (a Core Invariant), so
the block could only ever hold *references*, yet it lived inside the one object
whose job is fleet-wide defaults, mixing two concerns. Different kinds
(Machines, clusters, entitlements, add-ons) all needed to name the same secret,
but there was no first-class object to name. And the shapes were ad hoc: a pull
secret, a certificate, and a generated token had no common grammar, so each
consumer resolved credentials its own way.

`v1alpha1` may break cleanly (no migrations or shims), which made a structural
fix available rather than a compatibility layer.

## Decision

Secret material is a first-class `kind: Secret` object. `Environment` no longer
carries any `secrets` field; every consumer references a secret by
`metadata.name` through a `SecretRef` (resolved by name in the loaded state).

A `Secret` declares a closed **type set** and a **source union**:

- `spec.type` is one of a fixed seven-value set — `opaque`, `token`,
  `usernamePassword`, `dockerConfigJson`, `caBundle`, `tlsCertificate`,
  `sshKeyPair`. The set is closed; strict decode rejects any other value.
- `spec.source` is a presence union with three arms — `contextStore` (the
  default when the whole `source` block is omitted; the bytes live in the
  per-context encrypted secret store), `file` (a local path, or split
  `cert`/`key`/`privateKey`/`publicKey` paths), and `generated` (Bootwright
  synthesizes the material: tokens, self-signed certificates/CA bundles,
  credentials, and SSH key pairs, per the type). Generatable and self-signed
  types are a subset of the seven.

The union grammar itself — presence unions, the `Ref` suffix, reserved
spellings — is owned by ADR 0014; this ADR records the decision to *promote
secret material to its own kind* and to remove it from `Environment`. The
resolution namespace and per-type field rules are stated in
`specs/state-model.md`.

## Consequences

- Any kind that needs a credential names a `Secret` by a `Ref` field; there is
  no inline secret material anywhere in the API, and no `Environment.spec.secrets`
  block.
- The seven-value type set and the three source arms are the whole authoring
  surface for secrets. Adding an eighth type or a fourth source arm is an API
  break that needs its own ADR: strict decode rejects anything outside
  `api/v1alpha1.SecretTypes()`, and the generatable and self-signed subsets are
  enumerated separately (`SecretTypeGeneratable`, `SecretTypeSelfSigned`), so a
  new type lands in three places or not at all.
- Because the default source is `contextStore`, an author who declares only
  `kind: Secret` with a `type` and no `source` is opting into the encrypted
  per-context store; `file` and `generated` are explicit opt-outs.
- Superseded framing (secrets as an `Environment` sub-block) is removed from
  specs and docs rather than retained as a compatibility note.
