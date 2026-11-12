package diskselection

import (
	"fmt"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// PolicyFromValue consumes schema-validated authored intent, retaining bounds
// at the domain boundary as well so direct callers cannot bypass admission.
func PolicyFromValue(value api.Value) (Policy, error) {
	var p Policy
	m := value.Get("match")
	p.Match.Model = m.Get("model").Text()
	p.Match.ControllerPCIAddress = m.Get("controllerPCIAddress").Text()
	for name, target := range map[string]*int64{"minSizeBytes": &p.Match.MinSizeBytes, "maxSizeBytes": &p.Match.MaxSizeBytes} {
		if v := m.Get(name); v.Present() {
			n, ok := v.Int64()
			if !ok || n == 0 {
				return Policy{}, fmt.Errorf("disk capacity predicate is not a positive aligned integer")
			}
			*target = n
		}
	}
	for _, entry := range value.Get("exclude").Items() {
		n, ok := entry.Get("sizeBytes").Int64()
		if !ok {
			return Policy{}, fmt.Errorf("disk exclusion needs exact capacity")
		}
		p.Exclude = append(p.Exclude, DiskIdentity{WWN: entry.Get("wwn").Text(), SerialNumber: entry.Get("serialNumber").Text(), SizeBytes: n})
	}
	return p, p.Validate()
}
