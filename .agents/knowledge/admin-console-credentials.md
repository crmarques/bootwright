# Console administrator credentials

The installer console account and the administrator certificate kubeconfig are
different credentials. OpenShift and OKD write the generated console password
to `auth/kubeadmin-password`; the console username is `kubeadmin`. The
[credential contract](../../specs/cli/cluster-admin-credentials.md) owns Bootwright
custody, explicit disclosure and rotation.

The upstream [bootstrap authenticator](https://github.com/openshift/library-go/blob/release-4.22/pkg/authentication/bootstrapauthenticator/bootstrap.go)
reads the existing `kube-system/kubeadmin` Secret's bcrypt value. It refuses a
Secret created more than one hour after the namespace. Rotation updates the
existing Secret with UID, resourceVersion and hash tests; recreating a deleted
Secret cannot restore the original bootstrap account. This is a technical
implementation fact; the source alone establishes no vendor support policy.

Ceph's documented [password command](https://docs.ceph.com/en/latest/mgr/dashboard/#user-and-role-management)
accepts private standard input through `-i -`. Bootwright also retains the
password under the original cluster and bootstrap scope. Updating only the
Ceph account leaves its retained copy stale and makes later observations refuse
`dashboard-drift`. The explicit command updates both sides recoverably and
preserves the retained custody ID. The encrypted central pending candidate
allows a repeated command to finish after an interrupted remote update.

Synthetic evidence lives in `internal/reconciliation/lifecycle/admin_credentials_test.go`,
`internal/environment/inspection/console_test.go` and the native credential
tests under `ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/`.
