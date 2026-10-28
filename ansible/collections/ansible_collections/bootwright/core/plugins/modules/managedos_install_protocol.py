#!/usr/bin/python
"""Documentation entrypoint; the paired installation action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_install_protocol
short_description: Publish one installation capability phase
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which owns the
    inherited result channel the invoking Bootwright process reads.
  - The frozen Go request and that channel own authorization and evidence.
options:
  phase:
    description:
      - The capability phase being published.
      - C(prepared) publishes, before a registering apply registers the
        machine, that the machine holds this installation's marker; it names
        only that marker and waits until the runner has recorded it.
    type: str
    required: true
  group:
    description: The presentation group a progress phase reports.
    type: str
    required: false
  status:
    description: The status a progress phase reports for its group.
    type: str
    required: false
  reason:
    description:
      - The refusal a refused phase names to the runner before the run fails,
        which the runner reports as the installation's own diagnostic for its
        Machine. One of the target's pre-boot refusals C(hardware-mismatch),
        C(identity-mismatch) or C(machine-running), or C(media-changed-boot)
        for a boot image that no longer has the size and SHA-256 the
        operation froze.
      - C(foreign-installation) names a machine whose answered marker belongs
        to another installation. C(tpm-absent) names a missing positive TPM 2.0 proof.
      - C(firmware-setting-mismatch) names declared firmware settings a proof
        found different, and C(firmware-setting-not-applied) declared settings
        the firmware step did not apply.
      - C(ssh-policy-unavailable), C(ssh-policy-host-excluded),
        C(ssh-policy-fleet-excluded) and C(ssh-policy-generated-excluded)
        identify pre-effect SSH policy refusals.
      - C(fips-not-enabled) names an installed system that does not report
        the FIPS mode its installation enabled.
      - C(root-encryption-unproved) names an installed system that does not
        report its root filesystem on a TPM2-bound LUKS device although its
        installation encrypted the root disk.
      - C(registration-input) names an organization or activation key the
        registration worker refused as not one line of printable ASCII.
      - C(registration-foreign) names an installed system registered with a
        subscription consumer this installation did not create.
      - C(registration-refused) names a registration the subscription service
        refused with the organization and activation key.
      - C(registration-identity-collision) names another registered host the
        subscription service reports with the same hardware identity.
      - C(registration-unavailable) names a subscription service or
        registration tooling that did not answer, or did not complete the
        removal of the consumer this installation created.
      - C(registration-repositories) names subscription repository IDs the
        subscription service did not accept.
      - C(registration-trust) names a Satellite whose certificate the CA
        bundle its Entitlement declares does not verify.
      - C(registration-unreachable) names an installed system a removal could
        not read through its identity channel or its pinned connection, so
        the registration its installation created was not removed. It is
        reached only for a Machine whose apply proved its install marker,
        which precedes registration.
      - C(update-release-unavailable), C(update-release-mismatch),
        C(update-release-drift), C(update-no-repository), C(update-foreign),
        C(update-unavailable) and C(update-failed) name an update within the
        release the update or registration worker refused.
    type: str
    required: false
  detail:
    description:
      - Fixed text completing a refusal, never a value outside the closed
        character set the runner accepts. An update refusal names the
        version found, C(<found>@<repository>) or its closed cause.
    type: str
    required: false
  outcome:
    description: The terminal outcome a completion phase publishes.
    type: str
    required: false
  digest:
    description: The frozen request digest the evidence must name.
    type: str
    required: false
  observation:
    description:
      - The bounded observation of published content.
      - Its C(treeIdentity) is the digest of the image the published tree was
        extracted from, empty or 64 lowercase hexadecimal characters, which the
        evidence carries unchanged.
    type: dict
    required: false
  marker:
    description:
      - The install marker read from the guest through the identity operation.
      - A completion's evidence carries it, and a prepared phase publishes it
        as the before-state its runner records.
    type: str
    required: false
  hostKey:
    description: The guest's SSH host public key, captured out of band.
    type: str
    required: false
  address:
    description: The address the installed guest answered on.
    type: str
    required: false
  media:
    description: The image the management controller still presents, if any.
    type: str
    required: false
  power:
    description: The power state the management controller reported.
    type: str
    required: false
  reachable:
    description:
      - Whether the fleet account answered on the host key the machine
        reported. An observation proves this the same way an apply does.
    type: bool
    required: false
  fips:
    description:
      - Whether the installed system proved FIPS mode after its network
        converged.
    type: bool
    required: false
  fipsRequired:
    description:
      - Whether the frozen installer arguments enable FIPS mode, so a
        completion proves it.
    type: bool
    required: false
  rootEncryption:
    description:
      - Whether the installed system proved its root filesystem on a crypt
        device whose LUKS backing device carries a clevis tpm2 slot, after its
        network converged.
    type: bool
    required: false
  rootEncryptionRequired:
    description:
      - Whether the frozen request encrypts the root disk, so a completion
        proves it.
    type: bool
    required: false
  registration:
    description:
      - The consumer UUID and the ownership state the registration worker
        read or a removal left, C(owned), C(pending), C(absent), C(foreign),
        C(unregistered) or C(retained); a consumer is named only for an owned,
        a pending or an unregistered registration.
    type: dict
    required: false
  registrationRequired:
    description:
      - Whether the frozen request registers the installed system, so a
        completion proves an owned registration, or a removal that
        unregisters proves one C(unregistered), C(absent) or C(retained). A
        removal that does not unregister reads no registration and sets it
        false.
    type: bool
    required: false
  update:
    description:
      - What the update within the release did or what an observation read,
        C(state) C(none), C(absent), C(pending), C(upgraded), C(complete) or
        C(foreign), with C(release), C(transaction), C(packages),
        C(rebooted) and C(kernel) set only for a record's states.
    type: dict
    required: false
  updateRequired:
    description:
      - Whether the frozen request updates within its release, so a
        completion proves a complete update; any other completion proves
        state C(none).
    type: bool
    required: false
  privateDelivery:
    description:
      - Whether the installation published its installer image beneath its
        private subtree. Such an image names the private URL in its
        Kickstart, so a completion proves it withdrawn rather than in place.
    type: bool
    required: false
  removed:
    description: Whether the completion proves removal rather than presence.
    type: bool
    required: false
  observed:
    description:
      - Whether this publication is a read-only observation, which may report
        evidence that proves no postcondition rather than failing.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by bootwright.core.managedos_install_anaconda using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.managedos_install_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""
