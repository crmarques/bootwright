package contextfs

// checkpoint names the instant immediately before or after one durable effect
// of a publication, where the test-only fail hook may refuse, cancel or kill
// it. Every value is catalogued below, and
// TestEveryCheckpointIsACataloguedConstant refuses any other argument to
// Store.checkpoint.
type checkpoint string

const (
	checkpointAfterClientAreaDirectory       checkpoint = "after-client-area-directory"
	checkpointAfterContextDirectory          checkpoint = "after-context-directory"
	checkpointAfterContextReservation        checkpoint = "after-context-reservation"
	checkpointAfterControllerBundleCreate    checkpoint = "after-controller-bundle-create"
	checkpointAfterControllerBundleDirectory checkpoint = "after-controller-bundle-directory"
	checkpointAfterControllerBundleRetiring  checkpoint = "after-controller-bundle-retiring"
	checkpointAfterControllerDirectory       checkpoint = "after-controller-directory"
	checkpointAfterControllerRename          checkpoint = "after-controller-rename"
	checkpointAfterEvidenceRename            checkpoint = "after-evidence-rename"
	checkpointAfterInitialRegistryRecovery   checkpoint = "after-initial-registry-recovery"
	checkpointAfterOperationRename           checkpoint = "after-operation-rename"
	checkpointAfterRegistryRename            checkpoint = "after-registry-rename"
	checkpointAfterSecretImmutableRename     checkpoint = "after-secret-immutable-rename"
	checkpointAfterSecretRename              checkpoint = "after-secret-rename"
	checkpointAfterSecretUnlink              checkpoint = "after-secret-unlink"
	checkpointAppendOperationLog             checkpoint = "append-operation-log"
	checkpointBeforeBinding                  checkpoint = "before-binding"
	checkpointBeforeClientAreaAttribution    checkpoint = "before-client-area-attribution"
	checkpointBeforeClientAreaReservation    checkpoint = "before-client-area-reservation"
	checkpointBeforeClientAreaSealing        checkpoint = "before-client-area-sealing"
	checkpointBeforeContextReady             checkpoint = "before-context-ready"
	checkpointBeforeContextRmdir             checkpoint = "before-context-rmdir"
	checkpointBeforeContextSubtree           checkpoint = "before-context-subtree"
	checkpointBeforeContextUnlink            checkpoint = "before-context-unlink"
	checkpointBeforeControllerBundleRename   checkpoint = "before-controller-bundle-rename"
	checkpointBeforeControllerBundleSync     checkpoint = "before-controller-bundle-sync"
	checkpointBeforeControllerBundleUnlink   checkpoint = "before-controller-bundle-unlink"
	checkpointBeforeControllerBundleWrite    checkpoint = "before-controller-bundle-write"
	checkpointBeforeControllerRename         checkpoint = "before-controller-rename"
	checkpointBeforeEvidence                 checkpoint = "before-evidence"
	checkpointBeforeEvidenceRename           checkpoint = "before-evidence-rename"
	checkpointBeforeInitialRegistryRecovery  checkpoint = "before-initial-registry-recovery"
	checkpointBeforeMediaImageRemoval        checkpoint = "before-media-image-removal"
	checkpointBeforeMediaRecord              checkpoint = "before-media-record"
	checkpointBeforeMediaRecordRemoval       checkpoint = "before-media-record-removal"
	checkpointBeforeMediaRename              checkpoint = "before-media-rename"
	checkpointBeforeMediaRetainedRemoval     checkpoint = "before-media-retained-removal"
	checkpointBeforeMediaRetention           checkpoint = "before-media-retention"
	checkpointBeforeMediaStaging             checkpoint = "before-media-staging"
	checkpointBeforeMediaStagingPrune        checkpoint = "before-media-staging-prune"
	checkpointBeforeMediaStagingSync         checkpoint = "before-media-staging-sync"
	checkpointBeforeOperationRename          checkpoint = "before-operation-rename"
	checkpointBeforeRegistryRename           checkpoint = "before-registry-rename"
	checkpointBeforeReservation              checkpoint = "before-reservation"
	checkpointBeforeRetainedDependencies     checkpoint = "before-retained-dependencies"
	checkpointBeforeRevisionCleanup          checkpoint = "before-revision-cleanup"
	checkpointBeforeRevisionRemove           checkpoint = "before-revision-remove"
	checkpointBeforeRevisionRmdir            checkpoint = "before-revision-rmdir"
	checkpointBeforeSecretFileSync           checkpoint = "before-secret-file-sync"
	checkpointBeforeSecretImmutableRename    checkpoint = "before-secret-immutable-rename"
	checkpointBeforeSecretPrune              checkpoint = "before-secret-prune"
	checkpointBeforeSecretRename             checkpoint = "before-secret-rename"
	checkpointBeforeSecretUnlink             checkpoint = "before-secret-unlink"
	checkpointBeforeStageCollection          checkpoint = "before-stage-collection"
	checkpointConfirmOperationEntry          checkpoint = "confirm-operation-entry"
	checkpointCreateFile                     checkpoint = "create-file"
	checkpointMeasureOperationEntry          checkpoint = "measure-operation-entry"
	checkpointMkdir                          checkpoint = "mkdir"
	checkpointSyncContextFile                checkpoint = "sync-context-file"
	checkpointSyncDirectory                  checkpoint = "sync-directory"
	checkpointSyncFile                       checkpoint = "sync-file"
	checkpointSyncInitialRegistryFile        checkpoint = "sync-initial-registry-file"
	checkpointWriteFile                      checkpoint = "write-file"
)

// checkpoints lists every catalogued checkpoint once, in name order.
func checkpoints() []checkpoint {
	return []checkpoint{
		checkpointAfterClientAreaDirectory,
		checkpointAfterContextDirectory,
		checkpointAfterContextReservation,
		checkpointAfterControllerBundleCreate,
		checkpointAfterControllerBundleDirectory,
		checkpointAfterControllerBundleRetiring,
		checkpointAfterControllerDirectory,
		checkpointAfterControllerRename,
		checkpointAfterEvidenceRename,
		checkpointAfterInitialRegistryRecovery,
		checkpointAfterOperationRename,
		checkpointAfterRegistryRename,
		checkpointAfterSecretImmutableRename,
		checkpointAfterSecretRename,
		checkpointAfterSecretUnlink,
		checkpointAppendOperationLog,
		checkpointBeforeBinding,
		checkpointBeforeClientAreaAttribution,
		checkpointBeforeClientAreaReservation,
		checkpointBeforeClientAreaSealing,
		checkpointBeforeContextReady,
		checkpointBeforeContextRmdir,
		checkpointBeforeContextSubtree,
		checkpointBeforeContextUnlink,
		checkpointBeforeControllerBundleRename,
		checkpointBeforeControllerBundleSync,
		checkpointBeforeControllerBundleUnlink,
		checkpointBeforeControllerBundleWrite,
		checkpointBeforeControllerRename,
		checkpointBeforeEvidence,
		checkpointBeforeEvidenceRename,
		checkpointBeforeInitialRegistryRecovery,
		checkpointBeforeMediaImageRemoval,
		checkpointBeforeMediaRecord,
		checkpointBeforeMediaRecordRemoval,
		checkpointBeforeMediaRename,
		checkpointBeforeMediaRetainedRemoval,
		checkpointBeforeMediaRetention,
		checkpointBeforeMediaStaging,
		checkpointBeforeMediaStagingPrune,
		checkpointBeforeMediaStagingSync,
		checkpointBeforeOperationRename,
		checkpointBeforeRegistryRename,
		checkpointBeforeReservation,
		checkpointBeforeRetainedDependencies,
		checkpointBeforeRevisionCleanup,
		checkpointBeforeRevisionRemove,
		checkpointBeforeRevisionRmdir,
		checkpointBeforeSecretFileSync,
		checkpointBeforeSecretImmutableRename,
		checkpointBeforeSecretPrune,
		checkpointBeforeSecretRename,
		checkpointBeforeSecretUnlink,
		checkpointBeforeStageCollection,
		checkpointConfirmOperationEntry,
		checkpointCreateFile,
		checkpointMeasureOperationEntry,
		checkpointMkdir,
		checkpointSyncContextFile,
		checkpointSyncDirectory,
		checkpointSyncFile,
		checkpointSyncInitialRegistryFile,
		checkpointWriteFile,
	}
}
