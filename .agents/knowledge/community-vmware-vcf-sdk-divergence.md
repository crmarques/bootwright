# `community.vmware` 6.x declares vcf-sdk, Bootwright installs pyvmomi

Pre-existing and unrelated to any single feature; recorded so it is not
rediscovered as a symptom of whatever vSphere work is in flight.

`community.vmware` 6.x declares a Python dependency on `vcf-sdk>=9.0.0.0` —
Broadcom's renamed successor to the pyVmomi distribution. Bootwright's managed
virtualenv installs `pyvmomi==9.1.0.0` instead, and
`internal/preflight/vsphere.go` probes only `import pyVmomi`, which both
distributions satisfy because they ship the same top-level package.

Consequences today: none observed. Every module Bootwright calls imports from
`pyVmomi`, and the pinned `pyvmomi` version tracks the same upstream release as
the `vcf-sdk` the collection asks for. The divergence is invisible until either
the collection starts importing a module that exists only in the `vcf-sdk`
distribution, or a dependency resolver is introduced that enforces the declared
requirement instead of the pin.

What to do if it ever bites: the symptom would be an `ImportError` from inside a
`community.vmware` module for a submodule name the preflight probe does not
check — not a preflight failure, because the probe is satisfied by either
distribution. The fix is to move the pin to `vcf-sdk` rather than to loosen the
probe; widening the probe would only make the mismatch harder to see.

Do not "fix" this by adding both distributions to the same environment. They
install the same top-level package and the later install wins non-obviously.
