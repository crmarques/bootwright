# Redfish proxy bypass

**Symptom:** Redfish VirtualMedia discovery or BMC validation reports HTTP
`403` from URLs such as `/redfish/v1/Systems/<id>/VirtualMedia`, but an
operator-run `curl -k -u "$BMC_USER:$BMC_PASS"` against the same URL returns
the expected Redfish JSON.

**Root cause:** Bootwright applies proxy environment facts at play scope for
runtime installer and download operations. Ansible's `uri` module uses proxy
environment variables by default, and its Python proxy bypass implementation
does not treat CIDR entries such as `10.0.0.0/8` as matching concrete BMC IPs.
The proxy response can look like a BMC `403`, hiding that the credentials are
valid.

**Fix:** Keep Redfish management calls under the rendered proxy environment and
make that environment effective by expanding matching CIDR no-proxy entries into
literal known BMC IPs. Do not set `use_proxy: false` on Redfish `uri` tasks; a
BMC that is not covered by rendered `NO_PROXY` must still be allowed to use the
configured proxy.
