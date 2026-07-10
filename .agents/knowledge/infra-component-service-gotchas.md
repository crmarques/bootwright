# Infra-component service gotchas (registry, HAProxy, Squid, VIPs, libvirt dnsmasq)

**Registry htpasswd panic:** the Distribution registry treats the mere
presence of the `REGISTRY_AUTH_HTPASSWD_PATH` env var as "configure htpasswd
auth" and panics at startup when no htpasswd file exists. `credentialsRef`
is optional for a disconnected mirror, so
`infra_component_registry_mirror` emits the htpasswd auth vars ONLY when
credentials are configured — otherwise the container crash-loops under
`restart_policy` while podman reports it as started (misleading green).

**HAProxy nonlocal-bind sysctl is host-wide:** the
`net.ipv4.ip_nonlocal_bind` sysctl drop-in is one file shared by every
bootwright HAProxy on the host. Destroy removes it only when no bootwright
HAProxy container remains, so a partial-scope destroy that leaves another LB
in place does not strip nonlocal-bind out from under it on the next reboot.
A running HAProxy keeps its existing binds regardless of the live sysctl, so
deferring removal is safe at run time.

**Squid runtime UID:** the openEuler Squid 7.5 image refuses to run as root;
the container must run as the image's baked-in squid user (UID/GID 1000,
`bootwright_squid_runtime_uid`/`gid`). This value must stay in sync with the
squid image pin in `internal/render/components.go` — a new image pin can
change the baked-in UID.

**VIP detach must tolerate already-gone addresses:** frontends sharing one
VIP (e.g. api + apiInt on the same address) emit one detach entry EACH, and
the second `ip address del` fails with `Address not found` (newer iproute2)
or `Cannot assign requested address` (older); `Cannot find device`
additionally tolerates the bridge itself already being gone
(`cluster_network_load_balancer_vips/tasks/destroy.yml`).

**libvirt network must disable its dnsmasq DNS:** the libvirt network
template sets `<dns enable='no'/>` (which sets `port=0` in the
libvirt-spawned dnsmasq; DHCP still works). Without it, libvirt's
`bind-dynamic` dnsmasq grabs `:53` on EVERY address aliased onto the bridge
— including the gateway and the LB VIPs added by
`bootwright.core.cluster_network_load_balancer_vips` — and blocks the podman
dnsmasq container that serves the cluster's api/api-int/ingress records.

**DHCP option 42 is IPv4-only:** RFC 2132 option 42 carries IPv4 addresses
only, so the libvirt network template forwards only IP-shaped entries from
`bootwright_resolved_ntp_sources` into the dnsmasq
`dhcp-option=option:ntp-server` line; hostname-only NTP entries are skipped
there and reach the host via agent-config `additionalNTPSources` instead.
