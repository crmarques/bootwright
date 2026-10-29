# Managed infrastructure services

Run a Squid proxy, dnsmasq DNS server, chrony NTP server and HTTP/HTTPS artifact
server on the controller. This example provisions no guest, operating system or
cluster and needs no installation media. The services remain selected even
without consumers; they are managed containers with context-owned systemd
units.

## Worth noting

- Topology: one controller Machine (`controller.lab.example.test`, `192.0.2.1`)
  with direct egress, hosting proxy `3128/tcp`, DNS `53/tcp+udp`, NTP `123/udp`
  and the artifact server on `8443` (https) and `8080` (http). The bind address
  must already belong to the controller, and those sockets must be free.
- The managed proxy cannot supply the controller's initial downloads, so the
  controller Machine declares `proxy.direct`.
- The artifact server's `persistent` retention keeps published content until
  destroy; its serving certificate is the `artifact-server-tls` Secret,
  generated locally in the context.
- Placeholders to replace: `192.0.2.1`, `controller.lab.example.test`, the
  service bind addresses and certificate SANs, DNS `forwarders` (`192.0.2.53`)
  and NTP `upstreamSources` (`192.0.2.123`) with reachable upstreams chosen
  independently of these services.
- No Machine disks exist, so removal does not need `--authorize data-loss`.
