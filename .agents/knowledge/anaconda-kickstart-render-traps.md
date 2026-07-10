# Anaconda kickstart template rendering traps (ks.cfg.j2)

Jinja/kickstart interactions in
`machine_os_install_anaconda/templates/ks.cfg.j2` that produce corrupt or
credential-leaking kickstarts when regressed.

**trim_blocks eats the newline after `{% ... %}`:** Ansible renders templates
with `trim_blocks=True`, which strips the newline after every statement tag. A
kickstart command line that ENDS in a conditional therefore loses its trailing
newline and the next command collapses onto it — e.g.
`rhsm ... --proxy=...:8080lang en_US.UTF-8`. Precompute optional flags into
variables so each command line ends in a `{{ expression }}` and keeps its
newline; the `%packages` header options get the same treatment (a trailing
`{% endif %}` would glue the `@^...-environment` group onto the header line).

**`urlencode` does not escape `/`:** a proxy credential containing `/` would
terminate the `--proxy=` URL authority early; both user and password are
post-processed with `| urlencode | replace('/', '%2F')`. Full detail in
url-authority-gotchas.md.

**Proxy credential shape is asserted:** the machineOSInstall proxy credentials
file must hold a `user:password` pair. A generated secret always does; a
file/keyFile-sourced secret is arbitrary, so the template fails via
`undef(hint=...)` naming the secret and the required shape — instead of an
opaque Jinja index error deep in the machines phase when the split finds no
`:`.

**Anaconda has no no_proxy:** the kickstart `rhsm`/`url`/`repo` commands have
no no_proxy directive, so Bootwright decides per directive at render time
whether the directive's target host is bypassed. A directive gets `--proxy=`
only when its per-target flag is set (`rhsm.proxied`,
`installer.sourceProxied`); an internal Satellite, install tree, or mirror
matching no_proxy is fetched direct.

**Credentials never land in vars.yaml:** the credentialed proxy URL is built
inside the template from the unauthenticated `proxy.url` plus the credentials
file (`proxy.credentialsPath`), so the password appears only in the generated
kickstart, not in the rendered vars.
