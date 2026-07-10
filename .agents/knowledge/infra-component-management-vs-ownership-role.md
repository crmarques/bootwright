# Infra-component `management` values vs ownership-record `role`

Two vocabularies look alike and must not be conflated.

**Constraint:** `Environment.spec.infraComponents.*[]` entry `management`
accepts exactly `external` (a non-Bootwright endpoint that is only consumed —
a raw URL/address) and `managed` (this context provisions the service via
`componentRef` and may destroy it). `none` (`EnvironmentComponentNone`) is a
reserved component *name/ref sentinel* — it is never a `management` value,
and no catalog entry may be named `none`.

**Constraint:** A future `reference` management value — consume a sibling
context's owned service and contribute additive entries without provisioning
the base — is NOT accepted by the validators. Do not author
`management: reference`; it will fail validation.

**Why the confusion:** the live cross-context sharing mechanism uses the
same word for a different concept: `ComponentRoleOwner` /
`ComponentRoleReference` (`owner`/`reference` in `api/v1alpha1/types.go`)
are lifecycle roles stamped on a shared infra-component's ownership record
`role` field, not authorable management values. Their semantics, the
absent-reads-as-owner rule, and the hand-synced Go/Ansible literals are
covered in [ownership-records-store.md](ownership-records-store.md).
