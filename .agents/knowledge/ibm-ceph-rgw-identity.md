# IBM Ceph RGW identity integration

Facts behind the object gateway's LDAP authentication
([CephObjectGateway](../../specs/ceph-resources.md#cephobjectgateway-field-record),
[object gateway](../../specs/ceph.md#object-gateway)). Read offline in
`cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074` and the upstream v20.2.1
sources on 2026-11-25; the live rows below are still owed (B1833).

## Search filter

Without `@USERNAME@`, `LDAPHelper::auth` (`src/rgw/rgw_ldap.cc`) builds
`"(&(" + rgw_ldap_searchfilter + ")(" + rgw_ldap_dnattr + "=" + uid + "))"`,
so a parenthesized filter such as `(memberOf=...)` becomes the invalid
`(&((memberOf=...))(sAMAccountName=u))` and every LDAP sign-in fails. With the
token RGW substitutes it and adds nothing. The gateway therefore renders
`(&<filter>(<nameAttribute>=@USERNAME@))` and omits the option without a
filter (`rgwLDAPOptions` in `internal/storage/ceph/gateway.go`,
`TestAGatewayLDAPFilterIsRenderedWithTheUsernamePlaceholder`). RGW does not
escape the sign-in name in the filter; the directory filter still ANDs, and an
injected name never equals a CephObjectUser ID.

## Bind password file

`rgw_ldap_secret` is a file path read at start-up; a missing or unreadable file
is an empty password with no error, so the bind silently fails. RGW trims the
file (`boost::algorithm::trim`) and reads at most 1023 bytes; an all-whitespace
file is undefined behaviour. Binding refuses such a value
(`TestBindingRefusesALDAPBindPasswordRGWWouldTrimOrCut`). RGW sets no LDAP TLS
option or timeout, so libldap's `/etc/openldap/ldap.conf` (`TLS_CACERT`,
`TLS_REQCERT`, `NETWORK_TIMEOUT`, `TIMEOUT`) governs; the image's file holds only
`SASL_NOCANON on`, which a simple bind does not need.

## cephadm custom configs and `spec.config`

cephadm writes each `custom_configs` entry to
`/var/lib/ceph/<fsid>/custom_config_files/<daemon>/<basename(mount_path)>`, mode
0600, owned by the image's ceph user (167:167), and bind-mounts it with a plain
`-v src:dst`, without `:z` or `:ro`; two entries with one basename collide
(cephadm zipapp `_write_custom_conf_files`, `cephadmlib/deployment_utils.py`).
Whether SELinux lets RGW read the unrelabelled file is unproved offline; the
mount round compares each file's label with the daemon keyring's
(`test_a_file_labelled_unlike_the_keyring_is_unproved`). A document's `config`
map is cephadm's declarative option channel: `_apply_service_config`
(`cephadm/serve.py:841-905`) sets each differing option at
`client.rgw.<service_id>` before it deploys daemons and on every serve loop,
`orch rm` removes them (`module.py:3412`), and an unknown option is ignored with
only `CEPHADM_INVALID_CONFIG_OPTION`, which is why readiness reads every option
back from each daemon. `RGWSpec.MANAGED_CONFIG_OPTIONS` (`mds_join_fs`,
`rgw_zone`, `rgw_realm`, `rgw_zonegroup`, `rgw_frontends`) holds none of them.
A single-option `ceph config show <who> <option>` answers bare text, not JSON,
as `config get` does ([monitoring](ibm-ceph-monitoring.md)).

## Exposure

`ServiceSpec.to_json` emits `custom_configs` content and `spec.config`, so the
bind password shows in `ceph orch ls --export` and in the dashboard's
`GET /api/service/<name>` to every user with `hosts` read
(`dashboard/controllers/service.py`), as inline `ssl_key` and the oauth2-proxy
secrets already do; owner question Q154 holds the alternative. The STS key is
worse: `rgw_sts_key` has no file option, so it sits in `spec.config`, in the
configuration database (`ceph config dump`) and in each daemon's `ceph config
show`, readable by every Ceph administrator.

## STS

`rgw_sts_key` is the raw AES-128 key, 16 letters and digits (`rgw_sts.cc`), and
its built-in default is the literal `sts`, so a daemon that has not read the
spec's key yet reports `sts`, never nothing; readiness treats any value other
than the stored one as pending (`sts@rgw.<id>`). A generated `token` Secret is
unpadded base64url of at least 22 characters and never fits, so the key is a
context-stored opaque or token Secret, and an opaque one keeps a final newline
a shell `echo` adds. Every read of the key is compared with `hmac` and every
native diagnostic of `config dump` or `config show` carries the key as a
redaction secret, because `ceph_native.command` puts stdout into the diagnostic
when stderr is empty (`test_a_failing_native_read_of_the_key_is_redacted`).
RGW's STS engine only denies, like LDAP, so local keys never reach the
identity provider; it fetches the issuer's JWKS with libcurl, whose CA bundle
only `CURL_CA_BUNDLE` replaces as a whole (B1837), with no proxy option.
Providers and roles exist only through the IAM API (`CreateOpenIDConnectProvider`
needs thumbprints; radosgw-admin has no oidc-provider command), and IBM 9.9.1
deprecates tenant-level OIDC in favour of accounts.

## Live rows owed

The owner's B1833 gate records: the mount round passing on SELinux-enforcing
hosts, `ceph log last 200 audit` holding no bind password (B1834 adds: and no
STS key), and whether
cephadm re-sets the boolean `rgw_s3_auth_use_ldap` on every serve loop because
it compares the typed option with the string `true`.
