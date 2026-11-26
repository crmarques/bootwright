package ceph

import (
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	keptWithCluster = "it makes no call of its own: it needs its cluster step done, which refuses this teardown first, so it never ran and the cluster's teardown leaves nothing of it"
	keptNeverRan    = "nothing: the step never ran, so it wrote nothing inside or outside the cluster"
)

func keptWith(ctx context.Context, e lifecycle.Execution) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(KeptTeardown{Hosts: []KeptTeardownHost{}, Request: e.Block.RequestDigest, Version: KeptTeardownVersion})
	if err != nil {
		return nil, cephRequestInvalid.Fail("the Ceph kept-host teardown evidence cannot be encoded", "")
	}
	return data, nil
}

func destroyKeptWith(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	evidence, err := keptWith(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
}

func observeKeptWith(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	evidence, err := keptWith(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func keptWithRemoval(removal lifecycle.Removal, err error, description string, impacts, retains []string) (lifecycle.Removal, error) {
	if err != nil {
		return removal, err
	}
	removal.Description, removal.Impacts, removal.Retains = description, impacts, retains
	return removal, nil
}

func (SMBCapability) KeptHostsRefusal(block reconciliation.Block, records lifecycle.RemovalRecords, _ bool) string {
	r, err := DecodeSMBRequest(block.Request)
	if err != nil {
		return "its frozen request cannot be read"
	}
	return keptAccessRefusal("SMB resource "+r.Object+", whose shares and logins serve SMB clients", records)
}

func (SMBCapability) OnlineKeptHosts(block reconciliation.Block) ([]string, error) {
	r, err := DecodeSMBRequest(block.Request)
	return r.Machines, err
}

func (SMBCapability) KeptHostsContract() (string, string) {
	return KeptImplementation, KeptContentDigest()
}

func (c SMBCapability) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	removal, err := c.Removal(ctx, block)
	r, decodeErr := DecodeSMBRequest(block.Request)
	if err == nil {
		err = decodeErr
	}
	return keptWithRemoval(removal, err, "take back Ceph SMB resource "+r.Object+" of "+r.Cluster+" with its cluster's teardown from its kept hosts",
		[]string{"take back SMB resource " + r.Object + " with the cluster; " + keptWithCluster}, []string{keptNeverRan})
}

func (ConsumerCapability) KeptHostsRefusal(block reconciliation.Block, records lifecycle.RemovalRecords, _ bool) string {
	r, err := DecodeConsumerRequest(block.Request)
	if err != nil {
		return "its frozen request cannot be read"
	}
	return keptAccessRefusal("the CephX keys of consumer "+r.Name, records)
}

func (ConsumerCapability) OnlineKeptHosts(block reconciliation.Block) ([]string, error) {
	r, err := DecodeConsumerRequest(block.Request)
	return r.Machines, err
}

func (ConsumerCapability) KeptHostsContract() (string, string) {
	return KeptImplementation, KeptContentDigest()
}

func (c ConsumerCapability) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	removal, err := c.Removal(ctx, block)
	r, decodeErr := DecodeConsumerRequest(block.Request)
	if err == nil {
		err = decodeErr
	}
	return keptWithRemoval(removal, err, "take back Ceph consumer "+r.Name+" of "+r.Cluster+" with its cluster's teardown from its kept hosts",
		[]string{"take back consumer " + r.Name + " with the cluster; " + keptWithCluster}, []string{keptNeverRan})
}

func (ReplicationCapability) KeptHostsRefusal(block reconciliation.Block, records lifecycle.RemovalRecords, _ bool) string {
	r, err := DecodeReplicationRequest(block.Request)
	if err != nil {
		return "its frozen request cannot be read"
	}
	return keptAccessRefusal("the rbd-mirror peer credential of "+r.Primary.Cluster+" to "+r.Secondary.Cluster, records)
}

func (ReplicationCapability) OnlineKeptHosts(block reconciliation.Block) ([]string, error) {
	r, err := DecodeReplicationRequest(block.Request)
	return append(append([]string{}, r.Primary.Machines...), r.Secondary.Machines...), err
}

func (ReplicationCapability) KeptHostsContract() (string, string) {
	return KeptImplementation, KeptContentDigest()
}

func (c ReplicationCapability) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	removal, err := c.Removal(ctx, block)
	r, decodeErr := DecodeReplicationRequest(block.Request)
	if err == nil {
		err = decodeErr
	}
	return keptWithRemoval(removal, err, "take back RBD replication "+r.Object+" from "+r.Primary.Cluster+" to "+r.Secondary.Cluster+" with both clusters' teardowns from their kept hosts",
		[]string{"take back replication " + r.Object + " with both clusters, each of which this staged destroy takes back; " + keptWithCluster}, []string{keptNeverRan})
}

func (SignOnCapability) KeptHostsRefusal(block reconciliation.Block, records lifecycle.RemovalRecords, _ bool) string {
	r, err := DecodeSignOnRequest(block.Request)
	if err != nil {
		return "its frozen request cannot be read"
	}
	return keptAccessRefusal("the identity-provider sign-on "+r.Object+" to the management gateway", records)
}

func (SignOnCapability) OnlineKeptHosts(block reconciliation.Block) ([]string, error) {
	r, err := DecodeSignOnRequest(block.Request)
	return r.Machines, err
}

func (SignOnCapability) KeptHostsContract() (string, string) {
	return KeptImplementation, KeptContentDigest()
}

func (c SignOnCapability) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	removal, err := c.Removal(ctx, block)
	r, decodeErr := DecodeSignOnRequest(block.Request)
	if err == nil {
		err = decodeErr
	}
	return keptWithRemoval(removal, err, "take back Ceph sign-on "+r.Object+" of "+r.Cluster+" with its cluster's teardown from its kept hosts",
		[]string{"take back sign-on " + r.Object + " with the cluster; " + keptWithCluster}, []string{keptNeverRan})
}
