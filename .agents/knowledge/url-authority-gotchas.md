# URL authority construction gotchas (IPv6 literals, proxy credentials)

## Unbracketed IPv6 proxy authority

**Symptom:** clients dialing the rendered `httpProxy` URL fail with
net.SplitHostPort's `too many colons` error.

**Root cause:** a bare IPv6 literal in a URL authority must be bracketed
(`fd00::1` must render as `http://[fd00::1]:3128`); a plain `fmt` `%s:%d`
emits an unbracketed authority.

**Fix:** `ManagedProxyURL` builds the authority with `net.JoinHostPort`.
`TestManagedProxyURLBracketsIPv6` pins it.

## Unbracketed IPv6 Redfish host

**Symptom:** the Ansible BMC reachability probe fails with
`Port could not be cast to integer value as '140::99'` — far from the
authoring mistake.

**Root cause:** consumers join `/redfish/v1/Systems` onto the base URL, and
Python `urlsplit` misreads an unbracketed `fd00:140::99` by taking the
trailing group as a port.

**Fix:** `bracketRedfishHost` (internal/render/inventory/vars_boot.go) wraps
a bare IPv6 literal authority in square brackets. Already-bracketed hosts,
IPv4 literals, and DNS names pass through untouched.

## Proxy credentials in URL userinfo

**Symptom:** downstream `HTTP_PROXY` parsers break, or a kickstart
`--proxy=` URL terminates its authority early, when the proxy password
contains reserved characters such as `:` or `/`.

**Root cause:** credentials inlined into a proxy URL's userinfo segment must
be percent-encoded and round-trip decodable; a "raw" embedding leaks literal
reserved characters into the authority. On the Jinja side, the `urlencode`
filter leaves `/` unescaped (Python `quote` with `safe='/'`), so even an
encoded credential can still terminate the URL early.

**Fix:** Go renderers percent-encode userinfo (guarded in
`internal/render/installer/secrets_test.go`). Jinja templates must
post-process credentials with `| urlencode | replace('/', '%2F')` — pinned
in `ks.cfg.j2`.
