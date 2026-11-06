package agentinstall

import (
	"encoding/base64"
	"encoding/json"
	"maps"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func (c MediaCapability) volumeValues(execution lifecycle.Execution, request MediaRequest) (map[string]string, error) {
	config := maps.Clone(request.AgentConfig)
	if len(request.RootVolumes) != 0 {
		hosts, ok := config["hosts"].([]any)
		if !ok {
			return nil, refusal("lifecycle.state", "the frozen agent configuration has no host list", "")
		}
		cloned := make([]any, len(hosts))
		for i, entry := range hosts {
			host, ok := entry.(map[string]any)
			if !ok {
				return nil, refusal("lifecycle.state", "the frozen agent host is malformed", "")
			}
			cloned[i] = maps.Clone(host)
		}
		config["hosts"] = cloned
	}
	proofs := map[string]substrate.RootVolumeProof{}
	for _, binding := range request.RootVolumes {
		if c.identities == nil {
			return nil, refusal("lifecycle.state", "the logical root-volume proof reader is not configured", "")
		}
		pin, found, err := c.identities.PinnedIdentity(binding.Machine, execution.Proved)
		if err != nil {
			return nil, err
		}
		if !found || !binding.Ref.Matches(pin.RootVolume) {
			return nil, refusal("lifecycle.state", "Machine/"+binding.Machine+" has no matching logical root-volume proof", "apply the Machine stage before building media")
		}
		hosts, ok := config["hosts"].([]any)
		if !ok {
			return nil, refusal("lifecycle.state", "the frozen agent configuration has no host list", "")
		}
		matches := 0
		for _, entry := range hosts {
			host, ok := entry.(map[string]any)
			if ok && host["hostname"] == binding.Hostname {
				if _, exists := host["rootDeviceHints"]; exists {
					return nil, refusal("lifecycle.state", "the frozen host has competing root selectors", "")
				}
				host["rootDeviceHints"] = map[string]any{"wwn": pin.RootVolume.WWN}
				matches++
			}
		}
		if matches != 1 {
			return nil, refusal("lifecycle.state", "the volume binding does not name exactly one agent host", "")
		}
		if _, duplicate := proofs[binding.Hostname]; duplicate {
			return nil, refusal("lifecycle.state", "the media repeats a root-volume binding", "")
		}
		proofs[binding.Hostname] = pin.RootVolume
	}
	agent, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	roots, err := json.Marshal(proofs)
	if err != nil {
		return nil, err
	}
	return map[string]string{"agentConfigBase64": base64.StdEncoding.EncodeToString(agent), "rootVolumesBase64": base64.StdEncoding.EncodeToString(roots)}, nil
}
