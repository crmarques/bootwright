package capacity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

type Budget struct {
	Observer Observer
	Reserves Reserves
}

type budgetGroup struct {
	target      Target
	required    Quantities
	allocations []Allocation
}

func add(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, errors.New("invalid or overflowing resource budget")
	}
	return a + b, nil
}
func sum(a, b Quantities) (Quantities, error) {
	cpu, err := add(a.CPU, b.CPU)
	if err != nil {
		return Quantities{}, err
	}
	memory, err := add(a.Memory, b.Memory)
	if err != nil {
		return Quantities{}, err
	}
	disk, err := add(a.Disk, b.Disk)
	if err != nil {
		return Quantities{}, err
	}
	return Quantities{cpu, memory, disk}, nil
}

type observation struct {
	target   Target
	required Quantities
	facts    Facts
	reserve  Reserve
	credit   Quantities
	unproved struct{ memory, disk string }
	writes   map[string]int64
}

func (b Budget) Check(ctx context.Context, allocations []Allocation, writes []Write, dryRun bool) ([]Check, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	groups, err := allocationGroups(allocations)
	if err != nil {
		return nil, err
	}
	rows := make([]observation, 0, len(groups))
	for _, group := range groups {
		row, err := b.observeGroup(ctx, group, dryRun)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	hosts, disks, err := groupObservations(rows)
	if err != nil {
		return nil, err
	}
	if !dryRun {
		if err := b.addWrites(ctx, disks, writes); err != nil {
			return nil, err
		}
	}
	return budgetChecks(hosts, disks)
}

func (b Budget) addWrites(ctx context.Context, disks map[string]observation, writes []Write) error {
	hosts := map[string]bool{}
	for _, row := range disks {
		hosts[row.target.Host] = true
	}
	observed := map[string]Facts{}
	for _, write := range writes {
		if write.Label == "" || write.Path == "" || write.Bytes < 0 {
			return errors.New("capacity projection contains an invalid write")
		}
		if !hosts[write.Host] {
			continue
		}
		key := write.Host + "\x00" + write.Path
		facts, seen := observed[key]
		if !seen {
			var err error
			if facts, err = b.observeDisk(ctx, Target{Host: write.Host, Pool: write.Path, Context: write.Context, Local: write.Local, DiskOnly: true}); err != nil {
				return err
			}
			observed[key] = facts
		}
		var err error
		for id, row := range disks {
			if row.target.Host != write.Host {
				continue
			}
			switch {
			case !facts.DiskKnown || facts.DiskIdentity == "":
				row.facts.DiskKnown = false
				row.facts.Reason = cmp.Or(row.facts.Reason, "the filesystem the "+write.Label+" at "+write.Path+" lands on could not be observed")
			case facts.DiskIdentity != row.facts.DiskIdentity:
				continue
			case write.Unknown != "":
				row.facts.DiskKnown = false
				row.facts.Reason = cmp.Or(row.facts.Reason, write.Unknown)
			default:
				if row.required.Disk, err = add(row.required.Disk, write.Bytes); err != nil {
					return err
				}
				if row.writes == nil {
					row.writes = map[string]int64{}
				}
				if row.writes[write.Label], err = add(row.writes[write.Label], write.Bytes); err != nil {
					return err
				}
			}
			disks[id] = row
		}
	}
	return nil
}

func (b Budget) observeDisk(ctx context.Context, target Target) (Facts, error) {
	if b.Observer == nil {
		return Facts{}, nil
	}
	timeout := ProbeTimeout
	if !target.Local {
		timeout = RemoteProbeTimeout
	}
	probe, cancel := context.WithTimeout(ctx, timeout)
	facts, err := b.Observer.Observe(probe, target)
	expired := probe.Err()
	cancel()
	if ctx.Err() != nil {
		return Facts{}, ctx.Err()
	}
	if err != nil || expired != nil {
		return Facts{}, nil
	}
	return facts, nil
}

func allocationGroups(allocations []Allocation) ([]*budgetGroup, error) {
	groups := map[string]*budgetGroup{}
	seen := map[string]bool{}
	for _, allocation := range allocations {
		allocation.Disks = slices.Clone(allocation.Disks)
		if allocation.Machine == "" || allocation.Host == "" || allocation.Pool == "" || seen[allocation.Machine] {
			return nil, errors.New("capacity projection contains missing or duplicate identities")
		}
		seen[allocation.Machine] = true
		key := allocation.Host + "\x00" + allocation.Pool
		group := groups[key]
		if group == nil {
			group = &budgetGroup{target: Target{Host: allocation.Host, Pool: allocation.Pool, Context: allocation.Context, Local: allocation.Local}}
			groups[key] = group
		}
		if group.target.Local != allocation.Local || group.target.Context != allocation.Context {
			return nil, errors.New("capacity projection contradicts host placement")
		}
		var err error
		group.required, err = sum(group.required, allocation.Quantities)
		if err != nil {
			return nil, err
		}
		group.allocations = append(group.allocations, allocation)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]*budgetGroup, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		for _, allocation := range allocations {
			if allocation.Host == group.target.Host {
				allocation.Disks = slices.Clone(allocation.Disks)
				group.target.Allocations = append(group.target.Allocations, allocation)
			}
		}
		result = append(result, group)
	}
	return result, nil
}

func (b Budget) observeGroup(ctx context.Context, group *budgetGroup, dryRun bool) (observation, error) {
	row := observation{target: group.target, required: group.required}
	if b.Reserves != nil {
		row.reserve = b.Reserves(group.target)
	} else {
		row.reserve = group.allocations[0].Reserve
		for _, allocation := range group.allocations {
			if row.reserve != allocation.Reserve {
				row.reserve = Reserve{}
			}
		}
	}
	switch {
	case dryRun:
		row.facts.Reason = "live capacity was not observed in dry-run"
	case b.Observer == nil:
		row.facts.Reason = "no capacity observer is configured for this host"
	default:
		timeout := ProbeTimeout
		if !group.target.Local {
			timeout = RemoteProbeTimeout
		}
		probe, cancel := context.WithTimeout(ctx, timeout)
		facts, err := b.Observer.Observe(probe, group.target)
		expired := probe.Err()
		cancel()
		if ctx.Err() != nil {
			return observation{}, ctx.Err()
		}
		if err != nil || expired != nil {
			row.facts.Reason = "the bounded capacity observation did not complete"
		} else {
			row.facts = facts
		}
	}
	for _, allocation := range group.allocations {
		if !allocation.Allocated {
			continue
		}
		credit, proved := row.facts.Reusable[allocation.Machine]
		if reason, unproved := row.facts.Unproved[allocation.Machine]; unproved {
			reason = "live provider inventory has not proved Machine " + allocation.Machine + "'s reusable allocation: " + reason
			row.unproved.memory = cmp.Or(row.unproved.memory, reason)
			if !proved {
				row.unproved.disk = cmp.Or(row.unproved.disk, reason)
			}
		}
		if !proved {
			continue
		}
		if credit.Memory < 0 || credit.Disk < 0 || credit.Memory > allocation.Quantities.Memory || credit.Disk > allocation.Quantities.Disk {
			row.facts.MemoryKnown = false
			row.facts.DiskKnown = false
			row.facts.Reason = "live provider inventory has not proved this Machine's reusable allocation"
			continue
		}
		var err error
		row.credit.Memory, err = add(row.credit.Memory, credit.Memory)
		if err != nil {
			return observation{}, err
		}
		row.credit.Disk, err = add(row.credit.Disk, credit.Disk)
		if err != nil {
			return observation{}, err
		}
	}
	return row, nil
}

func groupObservations(rows []observation) (map[string]observation, map[string]observation, error) {
	hostGroups := map[string]observation{}
	diskGroups := map[string]observation{}
	for _, row := range rows {
		host, exists := hostGroups[row.target.Host]
		if !exists {
			host = row
			host.required.Disk = 0
		} else {
			var err error
			host.required, err = sum(host.required, Quantities{CPU: row.required.CPU, Memory: row.required.Memory})
			if err != nil {
				return nil, nil, err
			}
			host.credit.Memory, err = add(host.credit.Memory, row.credit.Memory)
			if err != nil {
				return nil, nil, err
			}
			host.unproved.memory = cmp.Or(host.unproved.memory, row.unproved.memory)
			host.facts.CPUKnown = host.facts.CPUKnown && row.facts.CPUKnown
			host.facts.MemoryKnown = host.facts.MemoryKnown && row.facts.MemoryKnown
			host.facts.Available.CPU = min(host.facts.Available.CPU, row.facts.Available.CPU)
			host.facts.Available.Memory = min(host.facts.Available.Memory, row.facts.Available.Memory)
			host.reserve.Configured = host.reserve.Configured && row.reserve.Configured && host.reserve.Quantities.CPU == row.reserve.Quantities.CPU && host.reserve.Quantities.Memory == row.reserve.Quantities.Memory
		}
		hostGroups[row.target.Host] = host
		identity := row.facts.DiskIdentity
		if identity == "" {
			identity = "unproved:" + row.target.Pool
			row.facts.DiskKnown = false
		}
		key := row.target.Host + "\x00" + identity
		disk, exists := diskGroups[key]
		if !exists {
			disk = row
			disk.required.CPU = 0
			disk.required.Memory = 0
		} else {
			var err error
			disk.required.Disk, err = add(disk.required.Disk, row.required.Disk)
			if err != nil {
				return nil, nil, err
			}
			disk.credit.Disk, err = add(disk.credit.Disk, row.credit.Disk)
			if err != nil {
				return nil, nil, err
			}
			disk.unproved.disk = cmp.Or(disk.unproved.disk, row.unproved.disk)
			disk.facts.DiskKnown = disk.facts.DiskKnown && row.facts.DiskKnown
			disk.facts.Available.Disk = min(disk.facts.Available.Disk, row.facts.Available.Disk)
			disk.reserve.Configured = disk.reserve.Configured && row.reserve.Configured && disk.reserve.Quantities.Disk == row.reserve.Quantities.Disk
		}
		diskGroups[key] = disk
	}
	return hostGroups, diskGroups, nil
}

func budgetChecks(hostGroups, diskGroups map[string]observation) ([]Check, error) {
	checks := []Check{}
	for _, row := range hostGroups {
		var err error
		if row.credit.Memory > 0 && row.facts.Available.Memory >= 0 {
			row.facts.Available.Memory, err = add(row.facts.Available.Memory, row.credit.Memory)
			if err != nil {
				return nil, err
			}
		}
		checks = append(checks, capacityCheck(row.target.Host, "cpu", "logical CPUs", row.required.CPU, row.facts.Available.CPU, row.reserve.Quantities.CPU, row.facts.CPUKnown, row.reserve.Configured, row.facts.Reason, ""), capacityCheck(row.target.Host, "memory", "bytes", row.required.Memory, row.facts.Available.Memory, row.reserve.Quantities.Memory, row.facts.MemoryKnown, row.reserve.Configured, row.facts.Reason, row.unproved.memory))
	}
	for _, row := range diskGroups {
		var err error
		if row.credit.Disk > 0 && row.facts.Available.Disk >= 0 {
			row.facts.Available.Disk, err = add(row.facts.Available.Disk, row.credit.Disk)
			if err != nil {
				return nil, err
			}
		}
		check := capacityCheck(row.target.Host+":"+row.target.Pool, "disk", "bytes", row.required.Disk, row.facts.Available.Disk, row.reserve.Quantities.Disk, row.facts.DiskKnown, row.reserve.Configured, row.facts.Reason, row.unproved.disk)
		if len(row.writes) > 0 {
			nameWrites(&check, row)
		}
		checks = append(checks, check)
	}
	slices.SortFunc(checks, func(a, b Check) int { return strings.Compare(a.Target+"/"+a.ID, b.Target+"/"+b.ID) })
	return checks, nil
}

func nameWrites(check *Check, row observation) {
	labels := slices.Sorted(maps.Keys(row.writes))
	guests := row.required.Disk
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		guests -= row.writes[label]
		parts = append(parts, fmt.Sprintf("%s %d", label, row.writes[label]))
	}
	groups := "guest disks " + strconv.FormatInt(guests, 10) + ", " + strings.Join(parts, ", ")
	switch check.Status {
	case Failed:
		check.Reason = fmt.Sprintf("%s and reserve %d require %d bytes; %d are available", groups, check.Capacity.Reserve, check.Capacity.Required+check.Capacity.Reserve, check.Capacity.Available)
		check.Next = "increase available capacity on this filesystem, reduce the declared guest budget, or place what the apply writes on another filesystem"
	case Passed:
		check.Reason = "available capacity covers " + groups + " and reserve " + strconv.FormatInt(check.Capacity.Reserve, 10) + " bytes"
	}
}

func capacityCheck(target, resource, unit string, required, available, reserve int64, known, configured bool, reason, unproved string) Check {
	result := Check{ID: "capacity." + resource, Target: target, Status: Unknown, Next: "provide a complete resource observation and configured controller/provider reserve", Capacity: &Capacity{Resource: resource, Unit: unit, Required: required, Available: available, Reserve: reserve, Observed: known}}
	total, err := add(required, reserve)
	switch {
	case err != nil || available < 0:
		result.Reason = "resource quantities are invalid or overflow"
	case !configured:
		result.Reason = "the controller/provider resource reserve is missing or contradictory"
		result.Next = "set InfraProvider.spec.libvirt.capacityReserve.cpu, memoryMiB and diskGiB explicitly; providers sharing resources must agree"
	case !known:
		result.Reason = reason
		if result.Reason == "" {
			result.Reason = "the resource observation is unavailable or contradictory"
		}
	case total > available && unproved != "":
		result.Reason = unproved
		result.Next = "repeat once the Machine's domain and process are stable"
	case total > available:
		result.Status = Failed
		result.Capacity.Deficit = total - available
		result.Reason = fmt.Sprintf("declared guests and reserve require %d %s; %d are available", total, unit, available)
		result.Next = "increase available capacity or reduce the declared guest budget"
	default:
		result.Status = Passed
		result.Reason = "available capacity covers declared guests and reserve"
		result.Next = ""
	}
	return result
}
