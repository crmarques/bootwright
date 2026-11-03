package installation

import (
	"cmp"
	"slices"
)

const MetadataClearWindow int64 = 128 << 20

var metadataClearReplicas = [...]int64{1 << 30, 10 << 30, 100 << 30, 1000 << 30}

type EraseRegion struct {
	Offset int64
	Length int64
}

func MetadataClearRegions(size int64) []EraseRegion {
	if size <= 0 {
		return nil
	}
	spans := [][2]int64{{0, min(MetadataClearWindow, size)}, {max(size-MetadataClearWindow, 0), size}}
	for _, offset := range metadataClearReplicas {
		if offset < size {
			spans = append(spans, [2]int64{offset, min(offset+MetadataClearWindow, size)})
		}
	}
	slices.SortFunc(spans, func(a, b [2]int64) int { return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1])) })
	regions := []EraseRegion{}
	for _, span := range spans {
		if last := len(regions) - 1; last >= 0 && span[0] <= regions[last].Offset+regions[last].Length {
			regions[last].Length = max(regions[last].Offset+regions[last].Length, span[1]) - regions[last].Offset
			continue
		}
		regions = append(regions, EraseRegion{Offset: span[0], Length: span[1] - span[0]})
	}
	return regions
}

func EraseCoverageBytes(target EraseTarget) int64 {
	switch target.Method {
	case EraseLogicalOverwrite:
		return target.SizeBytes
	case EraseMetadataClear:
		total := int64(0)
		for _, region := range MetadataClearRegions(target.SizeBytes) {
			total += region.Length
		}
		return total
	}
	return 0
}
