# Managed network service container runtimes

Observed while implementing M1f;
[Infrastructure services](../../specs/infrastructure-services.md#managed-network-services)
owns the required behavior. Each image below was pulled and driven by hand with
the planned Quadlet arguments before its role was written, because each item
here was a failed or surprising first attempt.

Qualified against podman 5.8.4 on Fedora 43 on 2026-09-13. Digests are pinned
in each capability's `catalog.go` under
`internal/infrastructureservices/{proxy,dnsserver,ntpserver}/`.

## Common

- All three images ship their own entrypoint wrapper or default command. None
  of them is used: every unit sets `Entrypoint` and `Exec` explicitly, so the
  frozen request alone decides what runs. This is the same lesson the artifact
  server's s2i entrypoint taught.
- All three run with `User=0` and `Network=host`, so the declared bind address
  and port are the real socket. Port 53 and port 123 cannot be bound otherwise.
- The generated unit is `Type=notify` with `--sdnotify=conmon`, so an active
  unit proves the container started, never that the daemon is answering.
  Readiness is probed per address.

## squid (`docker.io/ubuntu/squid`, 24.04)

- The image entrypoint is `entrypoint.sh` and its command is
  `-f /etc/squid/squid.conf -NYC`. Serving needs `Entrypoint=/usr/sbin/squid`
  with `Exec=-f /etc/squid/squid.conf -N`.
- `cache deny all`, `access_log none` and `pid_filename none` together let
  squid run with no writable cache, log or run directory, so the container owns
  nothing outside its read-only configuration.
- A bare `GET /` on the proxy port answers `HTTP/1.1 400 Bad Request`, because
  it is not a proxy request. That 400 is the expected positive readiness
  answer, exactly as the artifact server's empty-root 404 is: the probe accepts
  any well-formed status line rather than a particular code.
- A request from an address outside the derived client set answers `403`.

## dnsmasq (`docker.io/4km3/dnsmasq`)

- The image entrypoint is already `/usr/sbin/dnsmasq --keep-in-foreground`, but
  the configuration file must still be named explicitly with
  `--conf-file=/etc/dnsmasq.conf`; the image's own default path is not part of
  its published contract.
- `listen-address` alone is not enough: without `bind-interfaces` dnsmasq opens
  a wildcard socket and collides with any other resolver on the host.
- `no-resolv` and `no-hosts` keep it from reading the container's own resolver
  configuration and hosts file, so it answers only what the request froze.
- Readiness queries both UDP and TCP, because a resolver that answers only one
  transport is not usable by a cluster installer.

## chrony (`docker.io/dockurr/chrony`, chrony 4.9)

- The image entrypoint is `/entrypoint.sh`, which synthesizes a configuration
  from `NTP_DIRECTIVES` and a pool list. It is bypassed entirely:
  `Entrypoint=/usr/sbin/chronyd` with `Exec=-d -x -f /etc/chrony/chrony.conf`.
- `-x` keeps the container from disciplining the host clock; the log line
  `Disabled control of system clock` confirms it. It does **not** free the
  socket: a stock Fedora chronyd binds `0.0.0.0:123`, which covers every
  address, so the managed container's bind to a specific address contends with
  it. Give the host daemon explicit `bindaddress` lines, or stop it, before
  placing a managed time service on that host.
- `cmdport 0` avoids the command socket. Without it the daemon logs
  `Wrong permissions on /run/chrony` and disables the socket anyway.
- `local stratum 10 orphan` answers **stratum 0** until peers agree, which no
  client can use. Plain `local stratum 10` answers stratum 10 immediately and
  is what the template writes.
- A `driftfile` is unnecessary and pointing one at `/dev/null` logs a warning
  on every start.
- Readiness sends a mode-3 client packet and requires a mode-4 server reply.
  An unsynchronized stratum is not a failure.

Revisit when an image changes its user, entrypoint or configuration path, or
when a second implementation is added behind the same capability port.
