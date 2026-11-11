package lifecycle

import (
	"maps"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

type InstalledHostKey func(proofs []BlockEvidence, machine, address string) (trust.HostKey, error)

const InstalledHostKeyMaterial = "bootwright:installed-host-key"

func PinInstalledHost(execution Execution, placement machineref.Placement, proved InstalledHostKey) (Execution, machineref.Placement, error) {
	if !placement.Installed() {
		return execution, placement, nil
	}
	if proved == nil {
		return execution, placement, failure("lifecycle.state", "the installation proof reader of an installed placement is not bound", "")
	}
	proofs := execution.ApplyProved
	if len(proofs) == 0 {
		proofs = execution.Proved
	}
	key, err := proved(proofs, placement.Machine, placement.Address)
	if err != nil {
		return execution, placement, err
	}
	material := maps.Clone(execution.Material)
	if material == nil {
		material = map[string]secrets.Material{}
	}
	material[InstalledHostKeyMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(key.Line(placement.Address, placement.Port))})
	execution.Material = material
	placement.KnownHostsRef = InstalledHostKeyMaterial
	return execution, placement, nil
}
