package capacity

import (
	"math"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func ReservesFromCatalog(catalog api.Catalog, allocations []Allocation) Reserves {
	reserves := map[string]Reserve{}
	seen := map[string]bool{}
	for _, allocation := range allocations {
		key := allocation.Host + "\x00" + allocation.Pool
		provider, found := catalog.Find(api.InfraProvider, allocation.Provider)
		value := provider.Spec().Get("libvirt", "capacityReserve")
		cpu, cpuOK := value.Get("cpu").Int64()
		memory, memoryOK := value.Get("memoryMiB").Int64()
		disk, diskOK := value.Get("diskGiB").Int64()
		reserve := Reserve{}
		if found && cpuOK && memoryOK && diskOK && cpu >= 0 && memory >= 0 && disk >= 0 && memory <= math.MaxInt64/(1<<20) && disk <= math.MaxInt64/(1<<30) {
			reserve = Reserve{Configured: true, Quantities: Quantities{cpu, memory * (1 << 20), disk * (1 << 30)}}
		}
		previous := reserves[key]
		if seen[key] && (!previous.Configured || !reserve.Configured || previous.Quantities != reserve.Quantities) {
			reserve = Reserve{}
		}
		seen[key] = true
		reserves[key] = reserve
	}
	return func(target Target) Reserve { return reserves[target.Host+"\x00"+target.Pool] }
}
