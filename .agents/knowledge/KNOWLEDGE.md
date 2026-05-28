# Knowledge Index

Error and constraint knowledge extracted from code history. Match the reported symptom or error text, then load only the linked file.

| Category | Match symptoms / keywords | File |
| --- | --- | --- |
| Ansible / sudo | `Duplicate become password prompt`; apply hangs mid-play on sudo | [sudo-ansible-duplicate-prompt.md](sudo-ansible-duplicate-prompt.md) |
| Ansible / sudo | `sudo: sorry, you must have a tty to run sudo`; `sudo: a password is required`; `Install controller OS packages` | [ansible-sudo-requiretty.md](ansible-sudo-requiretty.md) |
| Ansible / remote tmp | `can't open file '<home-dir>/.ansible/tmp`; `[Errno 13] Permission denied`; `AnsiballZ_setup.py` | [ansible-remote-tmp-permission.md](ansible-remote-tmp-permission.md) |
| Ansible / sudo | `bootwright apply <target>` auto-sudo cannot find ansible; `ModuleNotFoundError` under sudo | [pip-user-sudo-pythonpath.md](pip-user-sudo-pythonpath.md) |
| Ansible / sudo | `Ensure tmp working directory exists`; permission denied under `<runtime-dir>/tmp/controller-clis`; `/var/lib/bootwright/contexts/<context>/runtime/tmp` | [ansible-controller-clis-root-temp.md](ansible-controller-clis-root-temp.md) |
| Ansible / Galaxy | `Unexpected Exception`; `'results'`; `Skipping Galaxy server`; `community.general` | [ansible-galaxy-results-build.md](ansible-galaxy-results-build.md) |
| Ansible / embed | Extracted bundle missing `_respawn.py`, `__init__.py`, dot/underscore files | [ansible-embed-underscore-files.md](ansible-embed-underscore-files.md) |
| Ansible / roles | `bootwright_current_cluster is undefined`; dynamic role import fails | [ansible-dynamic-role-dispatch.md](ansible-dynamic-role-dispatch.md) |
| Ansible / runtime | `Module result deserialization failed`; `rc=-15`; cleanup killed Ansible wrapper | [ansible-module-wrapper-pkill.md](ansible-module-wrapper-pkill.md) |
| Ansible / runtime | `Could not install packages due to an OSError`; `[Errno 2] No such file or directory`; `install ansible-core` | [ansible-managed-venv-rebuild.md](ansible-managed-venv-rebuild.md) |
| Ansible / callback | `Callback dispatch 'v2_runner_on_skipped' failed for plugin 'default'`; `Build proxy environment facts` | [ansible-callback-skipped-pin.md](ansible-callback-skipped-pin.md) |
| Ansible / transfer | `Fetch generated agent ISO to local runtime state`; large file fetch appears stuck; `.openshift_install.log` idle after ISO generation | [ansible-fetch-become-large-file.md](ansible-fetch-become-large-file.md) |
| Ansible / packages | `Failed to download metadata for repo`; `Cannot download repomd.xml`; `There are no enabled repositories`; `All mirrors were tried` | [ansible-dnf-unavailable-repo.md](ansible-dnf-unavailable-repo.md) |
| Provider / BMC | Apply hangs at BMC wait tasks; port already in use after provider rename | [stale-bmc-port-wait-hang.md](stale-bmc-port-wait-hang.md) |
| OpenShift install | Disconnected install fails TLS; agent never reaches SSH; image pull x509 error | [disconnected-trust-bundle-policy.md](disconnected-trust-bundle-policy.md) |
| OpenShift install | Mirror push x509 SAN mismatch after self-signed cert spec change | [self-signed-cert-drift.md](self-signed-cert-drift.md) |
| OpenShift install | Agent ISO cache stale; `cannot generate ISO image due to configuration errors` | [openshift-agent-iso-cache.md](openshift-agent-iso-cache.md) |
| OpenShift install | `Missing install-config.yaml`; `Fail when install-config is missing`; boot task after successful ISO generation | [openshift-install-config-consumed.md](openshift-install-config-consumed.md) |
| OpenShift install | `SQUASHFS error: Unable to read page`; `Unable to read fragment cache entry`; agent API never initializes after virtual media eject | [openshift-agent-iso-squashfs-detach.md](openshift-agent-iso-squashfs-detach.md) |
| OpenShift install | `v2GetClusterNotFound`; `Writing image to disk: 100%`; node boots agent ISO again | [openshift-agent-iso-reboot-loop.md](openshift-agent-iso-reboot-loop.md) |
| OpenShift install | `Only platform none and external supports 1 ControlPlane and 0 Compute nodes`; SNO renders `platform.baremetal` | [openshift-sno-platform-none.md](openshift-sno-platform-none.md) |
| OpenShift install | `hosts[0].interfaces: Required value`; libvirt profile-backed nodes have no MAC inventory | [openshift-agent-host-interfaces.md](openshift-agent-host-interfaces.md) |
| OpenShift install | `Bootstrap Kube API never initialized`; `no such host`; endpoint DNS missing; `providedBy` or `externalVip` | [external-dns-bootstrap.md](external-dns-bootstrap.md) |
| Libvirt / network | API VIP unreachable; `Bootstrap Kube API never initialized` | [libvirt-vip-bootstrap.md](libvirt-vip-bootstrap.md) |
| Libvirt / network | UUID mismatch; bridge already in use; stale libvirt XML | [libvirt-network-drift.md](libvirt-network-drift.md) |
| Redfish / boot | `InsertMedia` fails; `did not report the requested agent ISO`; `Inserted=False`; `VerifyCertificate PATCH status=412`; `ssl.SSLError`; emulator HTTPS mismatch; virtual media path mismatch; `Verify running libvirt virtual media source is absent`; `Confirm staged agent ISO fetch URL is reachable`; `HEAD status 404` | [redfish-virtual-media.md](redfish-virtual-media.md) |
| Redfish / boot | VirtualMedia discovery reports `status=403`; direct `curl -u` to the same BMC URL succeeds; play-level proxy environment intercepts Redfish | [redfish-proxy-bypass.md](redfish-proxy-bypass.md) |
| Redfish / boot | `Stage agent ISO at the BMC's fetch location`; `Gathering Facts`; `UNREACHABLE`; `Failed to connect to the host via ssh`; `localhost -> bastion`; wrong `ansible_user` | [redfish-local-artifact-staging.md](redfish-local-artifact-staging.md) |
| Redfish / boot | Reset(On) returns 204 but VM stays `shut off`; install loops on `no route to host` | [redfish-power-on-silent-noop.md](redfish-power-on-silent-noop.md) |
| Python / Ansible | Python 3.12 CIDR check returns false; VIP not matched to bridge CIDR | [python-312-cidr-filter.md](python-312-cidr-filter.md) |
