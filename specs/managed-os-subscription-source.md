# Subscription package source

A profile whose `spec.installer.anaconda.packageSource` selects
[`subscription`](api/machines.md#installer-arms) installs from the Satellite
its `spec.subscription` registers with: the installer registers the machine,
installs every package from the content the activation key selects, and hands
the registration to the installed system. No DVD and no hosted tree is read;
the boot image alone names the release, read from its `.discinfo` when it
carries no `.treeinfo` ([media store](managed-os.md#media-store)).
Everything this page does not state follows
[installation](managed-os.md#installation) and
[registration](managed-os.md#registration) unchanged.

## Admission and request

The profile declares `spec.subscription`, and its Entitlement is `managed`,
declares [`spec.rhsm.satellite`](api/secrets.md#rhsm) and selects no
`contentView` or `lifecycleEnvironment`, because the installer registers with
the activation key alone and installs from the view that key selects. Each
other shape refuses at `spec.installer.anaconda.packageSource.subscription`,
naming the Entitlement and the change that admits it. The artifact endpoint is
addressed by IP, as the [refusal table](managed-os.md#refusal-table) states,
because the `%pre` fetches before the installer resolves names; the Satellite's
own name is resolved through the name servers the `network` line carries.

The request freezes `registration.installSource` as `true` and no tree. The
installation is [private](managed-os.md#physical-private-publication) on every
substrate, so its image is published only beneath the attempt's token.

## Private delivery

Inside the boot, after the substrate proved the target and before the media is
inserted, the apply publishes the organization, the activation key and the
Satellite's CA bundle as `rhsm-organization`, `rhsm-activation-key` and
`rhsm-satellite-ca` in the attempt's private directory, each staged, labelled
and renamed as the recovery passphrase is, and fetches the first byte of each
through the listener. They leave with the private subtree once the
installation is proved, and a replay publishes none of them. An organization
or activation key holding a double quote, a single quote, a backslash or `#`
refuses `api.value` before any effect, naming the Secret and never a byte of
it, because the installer's registration command carries each as one Kickstart
token.

## Installer

A `%pre` brings the install interface up as the
[encrypting installation's](managed-os.md#root-disk-encryption) does, once
when both are selected, and fetches the three files with the artifact server's
certificate verified. It then:

1. writes the CA bundle to `/etc/rhsm/ca/bootwright-satellite.pem` in the
   installer's memory, where the subscription service verifies the Satellite
   against every certificate that directory holds;
2. sets `server.prefix` to `/rhsm`, `server.port` to `443`, `server.insecure`
   to `0` and `rhsm.repo_ca_cert` to that file through the subscription
   service's own configuration interface, the values the installed system's
   [worker](managed-os.md#registration) sets, and proves each reads back, so
   registration reaches the Satellite's subscription API and the package
   downloads verify the Satellite too;
3. refuses an organization or activation key outside 1 to 255 printable ASCII
   characters without space, quote, backslash or `#`;
4. writes `rhsm --organization=<organization> --activation-key=<key>
   --server-hostname=not-satellite:<hostname> --rhsm-baseurl=<contentBaseURL>`
   to `/tmp/bootwright-rhsm.ks`, mode 0600, which the Kickstart includes in
   place of an install source.

The two values exist only in shell variables and that file, both in the
installer's memory. The `%pre` uses only the tools the installer environment
holds.

**No Satellite provisioning script.** Given a plain server host name, Anaconda
downloads `http://<hostname>/pub/katello-rhsm-consumer` without verifying
anything and runs it as root in the installer and in the installed system.
The `not-satellite:` prefix is the installer's own switch that skips it:
Anaconda removes the prefix, sets the server and the content root, and
registers. Bootwright supplies what that script would have, the CA and the
settings above, over verified TLS, so nothing is fetched over HTTP and nothing
the Satellite serves is executed. An installer that does not know the prefix
passes it to the subscription service as a host name, and the registration
fails closed.

**Release path.** The installer expands `$releasever` to its boot image's
version, the profile's `spec.os.version`, such as `9.8`. The content view the
activation key selects therefore serves BaseOS and AppStream at that minor's
path, unless the activation key sets a release, which the subscription service
writes into the repository URLs literally.

## Installed system

Anaconda copies the consumer and entitlement certificates, `redhat.repo` and
`rhsm.conf` to the installed system. A `%post` outside the installed root
copies the CA bundle to the same path there, mode 0644. The marker's `%post`
proves the consumer certificate present and writes
`/etc/bootwright/registration.json` as
`{"consumer":"","request":"<request digest>"}`, mode 0600, the
[ownership](managed-os.md#registration) record of a `pending` registration; the
adapter substitutes the digest. The first apply's registration worker
therefore adopts the consumer the installer created and registers nothing,
writes the CA and the subscription-manager settings as on every apply, and
applies the release and the repositories; destroy unregisters it as any owned
consumer.

## Failure

A registration the Satellite refuses, a certificate the CA bundle does not
verify, a Satellite the install network cannot resolve or reach, a content
view serving no repository at the release path, and a Satellite already
holding a host with the Machine's hardware identity each stop the installer on
the Machine's console, so the apply fails when the
[installer budget](managed-os.md#budgets) ends, and its message names those
causes. An installer that registered and then failed leaves a host on the
Satellite that no record on the Machine names; it is removed with the
Satellite's own tools before the next apply, which would otherwise meet it as
that last cause.

## Sources and qualification

[RHEL 9 Kickstart `rhsm`](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/automatically_installing_rhel/kickstart-commands-and-options-reference_rhel-installer),
[Anaconda Satellite provisioning](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/subscription/satellite.py),
[registration and the `not-satellite:` prefix](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/subscription/runtime.py),
[token transfer](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/subscription/installation.py),
[`$releasever`](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/payloads/payload/dnf/utils.py)
and the [RHSM configuration interface](https://github.com/candlepin/subscription-manager/blob/main/src/rhsmlib/dbus/objects/config.py).
The installer's behavior is read from those sources and is not proved in-tree;
its emulated rehearsal is the operator gate of [B1548](milestones/m1.md#b1548).
