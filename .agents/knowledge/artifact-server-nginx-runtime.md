# Managed artifact-server nginx runtime

Observed while defining M1e;
[Infrastructure services](../../specs/infrastructure-services.md#managed-artifact-serving)
owns the required behavior. This page records what the selected image and the
host service manager actually demand, because each item below was a failed or
surprising first attempt.

Selected image: `registry.access.redhat.com/ubi9/nginx-124`, pinned by digest
in `internal/infrastructureservices/artifactserver/catalog.go`. Qualified
against podman 5.8.4 on Fedora 43 with nginx 1.24.0 inside the image.

- The image is an s2i builder: its entrypoint is `container-entrypoint` and its
  default command prints usage. Serving requires an explicit
  `Entrypoint=/usr/sbin/nginx` with `Exec=-g "daemon off;"`. Systemd renders the
  quoted argument as `daemon\x20off;` in the generated `ExecStart` and unescapes
  it before exec, so the quoting is correct as written.
- The image declares `USER 1001`, which cannot bind a port below 1024. The unit
  sets `User=0` so the declared listener ports are served exactly as authored.
  Workers still drop privileges through the configuration.
- `user default;` fails at startup with `getgrnam("default") failed`: the image
  has user `default` (1001) in group `root` (0) and no group of that name. The
  configuration must name both, `user default root;`. The `nginx` user (998)
  also exists but cannot reach `/var/lib/nginx`, which is `0770 default:root`,
  so it is the wrong worker identity for this image.
- Writable runtime paths (`/run`, `/var/lib/nginx`, `/var/log/nginx`) are group
  `0` writable in the OpenShift style, which is why the worker group must be
  `root` rather than a private group.
- An empty served root answers `HTTP/1.1 404 Not Found` on `/` with
  `autoindex off` and `try_files $uri =404`, and no `index` directive. That 404
  is the expected positive readiness answer until content publication exists,
  so the probe accepts any well-formed status line rather than `200`.
- The generated unit is `Type=notify` with `--sdnotify=conmon`, so an active
  unit proves the container started, never that nginx is listening. Readiness
  must be probed separately, which is why the capability's evidence requires a
  per-listener connection rather than the unit's state.
- The generated `ExecStart` already carries `--rm`, so a normal stop removes the
  container. The inverse still removes the container explicitly, because an
  abnormally terminated unit can leave one behind.
- The TLS readiness check compares SHA-256 over the peer certificate in DER
  form against the bound certificate's DER digest. Verified equal for a
  self-signed P-256 certificate served by this image.
- Workers open served files as `default` (1001) with group `root` (0), and the
  container is rootful with no user namespace, so those are host identities. A
  file `0600 root:root` is therefore unreadable by the worker; that is inferred
  from the configuration, not yet observed on a host (X14's fetch-through probe
  reports the status a fetch returns). Private files are published `0640 root:root`:
  owning them by uid 1001 instead would grant whichever host account holds that
  uid. `tests/unit/test_served_content_is_readable.py` ties every served file
  task to the worker the template names.
- The publication probe (`ansible.builtin.uri` with `ca_path` on the server's
  `tls/server.crt`, `Range: bytes=0-0`, `use_proxy: false`) was run locally,
  not against this image: ansible-core 2.21.4 on CPython 3.13.15 with OpenSSL
  3.5.8, a Python listener, and a certificate built from the same template as
  `secret generate` (self-signed P-256, `digitalSignature` only, server
  authentication, not a CA). It verified with the default flags and also with
  partial-chain verification cleared, and answered `206`. A foreign certificate
  and a hostname mismatch both returned status `-1` with
  `CERTIFICATE_VERIFY_FAILED` in `msg`; a refused connection and a missing
  `ca_path` file both returned `-1` without it. None of these messages named
  the URL. Because `status_code` lists both `200` and `206`, the module never
  reads the body of a `200`, so a server that ignores the range does not
  stream the image into memory.

Revisit when the image stream changes its user, entrypoint or writable-path
layout, or when a second server implementation is added behind the same
capability port.

## Why the Ansible process mechanics are not shared

M1e was planned to extract the process mechanics of
`internal/controller/ansiblelocal/runner_linux_amd64.go` into a shared
technical package and bind both the controller and service adapters to it.
That extraction was evaluated and rejected:

- A technical package may depend only on other technical packages, so every
  value the runner receives (`prerequisites.PythonLaunch`, the bundle location,
  the frozen request) would have to be re-declared and translated at both call
  sites, adding a whole conversion layer to remove a hundred lines.
- The drain and cancellation logic is not generic. It is entangled with M1d's
  native-transaction authorization: an authorized package transaction must be
  allowed to outlive cancellation and is drained on the longer grace period,
  while everything else is killed immediately. A service operation has no such
  phase, so the shared abstraction would carry a parameter that only one
  consumer ever sets.
- M1d's runner is qualified and its interruption behavior is covered by
  existing tests. The refactor's only benefit is deduplication, which does not
  justify the regression risk to the one path that installs host packages.

The service adapter therefore owns its own simpler boundary: cancellation
always terminates the process group, because no service effect is ever
authorized to outlive it.
