# Python 3.12 CIDR checks return false

**Symptom:** Managed load-balancer VIPs are not assigned to the libvirt bridge, and API VIP traffic is unreachable even though the VIP falls inside the configured CIDR.

**Root cause:** `ansible.utils.in_any_network` can return `False` on Python 3.12 because an internal helper reads a removed private `ipaddress` attribute and swallows the resulting exception.

**Fix:** Do not reintroduce CIDR matching in Ansible. The renderer now projects each load-balancer frontend's VIP attachment, including libvirt bridge and prefix, so `network_vips` does not need a CIDR test plugin.
