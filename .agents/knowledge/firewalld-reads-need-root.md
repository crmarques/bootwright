# Firewalld reads need an administrator

On the owner's three-node Ceph run (2026-11-03, RHEL guests with
`security.firewall.enabled`), firewalld refused every read to the non-root
access user, including the polkit `org.fedoraproject.FirewallD1.info` action:
`firewall-cmd --list-all` printed `NotAuthorizedException: Not Authorized(uid):
org.fedoraproject.FirewallD1.info` and `Authorization failed. Make sure polkit
agent is running or run the application as superuser.`, while
`sudo -n firewall-cmd --list-all` printed the zone. A non-interactive SSH
session has no polkit agent, so no `auth_admin` action can be answered.

Fedora's shipped policy grants `info` to any caller and gates zone and service
settings reads behind `config.info` (`auth_admin_keep`); the RHEL guest denied
both. Never infer a guest's polkit policy from the controller host's files.

Decision: `preflight storage-cluster` runs every `firewall-cmd` read as root,
through `sudo -n` once `host.privilege` proved it yields root
([Ingress host firewall](../../specs/ceph.md#ingress-host-firewall)). The
apply-time check already runs with become. `systemctl show` and `ip -json`
need no privilege and stay bare, so an inactive firewalld still passes on a
host without sudo.

Code: [guests.go](../../internal/storage/preflight/guests.go);
test `TestGuestFirewallReadsRunAsRootBecauseFirewalldRefusesZoneReadsToAUser`
in [firewall_test.go](../../internal/storage/preflight/firewall_test.go);
item [B1065](../../specs/milestones/m2.md#b1065).
