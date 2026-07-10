# `render storage` scoping never emits OpenShift inputs

**`render storage` is deliberately narrower than a full storage resolve.**
`StorageRenderState` (`internal/clusteraccess/select.go`) resolves the storage
`--clusters` scope, filters to the selected `StorageCluster`s plus their
backing machines/providers, and then explicitly nils `ContainerClusters` so
`render storage` never emits OpenShift installer inputs. Unlike
`Resolve(state, "storage-cluster", scope)`, it does not follow the
data-foundation attachment edge, so a `StorageCluster`'s OpenShift consumers
are never pulled into the render set. `TestStorageRenderStateDropsContainerClusters`
guards this with a phantom container cluster.

**Top-level render accepts storage cluster names.** The tool-input render
(context and portable `--input-dir` modes) resolves `--clusters` against the
`all` target so both `ContainerCluster` and `StorageCluster` names are accepted,
matching the flag help and `apply`. It previously hard-wired the
container-cluster target, so naming a `StorageCluster` failed with
`unknown cluster(s)` even though the bundle covers storage; the rejection
message now lists storage roots as available. Guarded by
`TestRenderOutputDirClustersAcceptsStorageCluster`.
