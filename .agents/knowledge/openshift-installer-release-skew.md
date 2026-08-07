# One installer binary serves every context, and only bastion setup refreshes it

**Symptom:** A cluster declares one release but installs another. The installer
log opens with a version that is not the declared one — `OpenShift Installer
4.21.15` for a cluster whose `spec.distribution.release.version` is `4.21.26` —
followed by `Using internal constant for release image
quay.io/openshift-release-dev/ocp-release@sha256:…`. Nothing fails: the ISO
builds, the hosts register, and the skew only surfaces much later in an operator
message such as `Cluster operator machine-api Degraded is True with
SyncingFailed: Failed when progressing towards operator: 4.21.15`.

**Root cause:** The agent workflow does not pin a release image. It runs
`openshift-install agent create image`, and that binary embeds the release
payload it was compiled against, so **the binary is the version pin**. The
binary lives at one hardcoded path — `DefaultControllerCLIInstallDir()` returns
the literal `/usr/local/bin`, with no context, cluster, or version component —
and holds exactly one version at a time for every context on the controller.

Only `bootwright bastion setup` writes it: `planControllerCLIInstall` has a
single caller in `newBastionSetupCmd`, and the `controller_openshift_tools` role
is reachable only from `workflow_bastion_apply_tools.yml`. No apply path runs
it. Bumping a release in desired state therefore changes what the cluster
*declares* without changing what actually builds its ISO, and the download
itself is not at fault — it is version-scoped and sha256-verified. The stale
copy is whatever the last `bastion setup` laid down, commonly a lower version
still declared by another context on the same controller.

**Fix:** The agent install stage now reads `openshift-install version` and
refuses when it does not match the cluster's declared release, naming the
binary, both versions, and `bootwright bastion setup` as the repair. Re-run
`bootwright bastion setup` with the cluster in scope after any release bump.
A FIPS cluster needs the `openshift-install-fips` build, which is only laid down
when the bastion was set up with a FIPS cluster in scope.

Pinned by `TestAgentInstallRefusesAnInstallerBinaryThatDoesNotMatchTheDeclaredRelease`
in `internal/repo/checks/ansible_agent_install_test.go`.
