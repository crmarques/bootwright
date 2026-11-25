# IBM Ceph management sign-on

Facts about the management gateway's single sign-on through oauth2-proxy and
the dashboard's OAuth2 mode, read offline in the image
`cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074` (`podman run --rm --network
none --entrypoint grep|sed|python3 ...`) on 2026-11-25 unless marked upstream.
Paths are image paths under `/usr/share/ceph/mgr/`. Required behavior lives in
[management gateway](../../specs/ceph.md#management-gateway) and
[gateway sign-on headers](../../specs/ceph.md#gateway-sign-on-headers). Another
build may differ; a difference is absorbed by the general flow or becomes a
declared seam
([version specializations](version-specializations.md#adding-a-specialization)).

## What turns sign-on on

Observation: `MgmtGatewaySpec.enable_auth` is stored but no cephadm code reads
it. Sign-on is on exactly while oauth2-proxy daemons exist:
`cephadm/services/mgmt_gateway.py:122` renders `'enable_oauth2_proxy':
bool(oauth2_proxy_endpoints)` and `cephadm/module.py:841` tracks the proxy as
the gateway's dependency. Adding or removing the oauth2-proxy service
reconfigures the gateway, Grafana (`[auth] disable_login_form`,
`[auth.proxy]`) and Prometheus and Alertmanager basic authentication
(`cephadm/services/monitoring.py:376,626`, `enable_basic_auth: not
oauth2_enabled`) through cephadm's dependencies, with no change to the gateway
document.

Decision: Bootwright keys the sign-on corrections on a declared oauth2-proxy,
not on `enable_auth`, and a gateway daemon rendered before the proxies ran is
reconfigured by the route proof
([gateway sign-on headers](../../specs/ceph.md#gateway-sign-on-headers)).

## Removing the proxy turns the dashboard's SSO off

Observation: removing the last running oauth2-proxy daemon makes cephadm call
the dashboard's `disable_oauth2_sso` (`cephadm/services/oauth2_proxy.py:88-110`,
`post_remove`), which sets the SSO protocol to local and `sso_oauth2` false
(`dashboard/module.py:545`). `dashboard sso enable oauth2` answers `EPERM`
unless an oauth2-proxy daemon runs (`dashboard/services/sso.py`).
`python3-saml` and `jmespath` are present in this image, so `dashboard sso
disable` works there.

Decision (planned by the identity program, D287): a fallback to local sign-in
removes the proxy and then proves `sso_oauth2` false, running `dashboard sso
disable` only if cephadm did not.

## No local dashboard sign-in while OAuth2 is on

Observation: while the dashboard's SSO protocol is OAuth2 there is no local
dashboard sign-in: the UI redirects to `auth/oauth2/login`, and a locally
issued token lacks `sub` and fails (`dashboard/services/auth/auth.py:167-168`).

Decision (planned by the identity program, D287): local access returns only
after sign-on is suspended, never beside it.

## Forged dashboard token

Observation (security): in OAuth2 mode `JwtManager.decode` skips both the
algorithm and the HMAC check (`dashboard/services/auth/auth.py:144-168`) and
maps the token's `sub` onto a local user, so an unsigned `alg: none` token with
`sub: admin` decodes and resolves the local `admin`. The dashboard reads the
token from the client's `token` cookie before the gateway's `X-Access-Token`
header (`dashboard/services/auth/oauth2.py:62-65`, `get_token`). The gateway's
oauth2 `/` location (`cephadm/templates/services/mgmt-gateway/external_server.conf.j2:78-113`)
forwards client cookies. So (a) through the gateway, any user the identity
provider lets in can become the dashboard `admin` by sending a forged `token`
cookie, and (b) anyone who reaches a manager's dashboard port 8443 directly
can. The template is byte identical in `ceph-9-rhel10:v9.9.2-26947` (sha256
`fec000edde5fba3db28f953601188f87c6a4a3ae2a4e237ceacbb59807cba444`).

Offline reproduction, inside the image with no network, no cluster and no
manager: a script that sets `UNITTEST=true`, puts `/usr/share/ceph/mgr` first
on `sys.path`, stubs the module `tests` (an empty module with `mock =
unittest.mock`) and the module `ceph_module` (a module whose `__getattr__`
returns `type(name, (object,), {})`), imports `dashboard`, sets
`dashboard.mgr.SSO_DB` to an object whose `protocol` is `AuthType.OAUTH2`, and
calls `JwtManager.decode(token, secret)` with an unsigned token: base64url of
`{"alg": "none", "typ": "JWT"}`, a dot, base64url of `{"sub": "admin", "jti":
"x", "iat": <now>, "exp": <now + 600>}`, and a trailing dot, against any
secret. It returns the claims with `username` `admin`. With `protocol` set to
`AuthType.LOCAL` the same call raises `InvalidAlgorithmError`. Run it with
`podman run --rm --network none -v <script>:/forge.py:ro,Z --entrypoint python3
<image> -I /forge.py`; the script is not committed.

Decision: the gateway's dashboard location clears the client's `Cookie`
header, so only the gateway's `X-Access-Token` reaches the dashboard
([record](version-specializations.md#ceph-gateway-sign-on-headers)). The
direct port stays exposed while sign-on is on; its answer is the owner's
([B1829](../../specs/milestones/m2.md#b1829), Q153). Both facts are worth
reporting to IBM.

## Client-chosen Grafana role

Observation (security): the gateway's oauth2 `/grafana` location sets
`proxy_set_header X-WEBAUTH-ROLE $http_x_auth_request_role;`
(`external_server.conf.j2:135`), the client's own request header, and
`cephadm/templates/services/grafana/grafana.ini.j2` maps `headers =
Role:X-WEBAUTH-ROLE` under `[auth.proxy]` (whose `whitelist` is the gateway
addresses). A signed-in user therefore chooses their own Grafana role, Admin
included.

Decision: the gateway sends the fixed role `Viewer`
([record](version-specializations.md#ceph-gateway-sign-on-headers)).

## Identity provider roles

Observation: the dashboard maps identity provider roles onto system roles only
(`Role.map_to_system_roles`). With Keycloak the default path reads
`resource_access.*[?@!='account'].roles[] || realm_access.roles[]`
(`dashboard/services/auth/oauth2.py`, `get_user_roles`); aliases include
`administrator|admin` and `read-only|read|guest|monitor`. A `roles_path` set
on a fresh SSO database is lost on a manager restart (upstream reading: the
`sso.py` save of a Saml2 configuration).

Decision (planned by the identity program, D287): Bootwright exposes no
`roles_path`.

## oauth2-proxy needs its issuer at start-up

Observation (upstream, oauth2-proxy v7.6.0, cephadm's default image):
oauth2-proxy performs OIDC discovery at start-up and exits when the issuer is
unreachable (`providers.go`, `provider_verifier.go`), so a proxy restarted
during an identity provider outage does not run.

Decision (planned by the identity program, D287): a fallback never contacts
the identity provider and must work with it down.
