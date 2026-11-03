package installation

import (
	"slices"
	"testing"
)

func TestMetadataClearRegionsMatchTheSharedVectors(t *testing.T) {
	const window = 134217728
	for _, test := range []struct {
		size    int64
		regions []EraseRegion
		covered int64
	}{
		{4096, []EraseRegion{{Offset: 0, Length: 4096}}, 4096},
		{window, []EraseRegion{{Offset: 0, Length: window}}, window},
		{2*window + 4096, []EraseRegion{{Offset: 0, Length: window}, {Offset: 134221824, Length: window}}, 268435456},
		{1073741824, []EraseRegion{{Offset: 0, Length: window}, {Offset: 939524096, Length: window}}, 268435456},
		{1073741824 + window/2, []EraseRegion{{Offset: 0, Length: window}, {Offset: 1006632960, Length: window}}, 268435456},
		{2000398934016, []EraseRegion{{Offset: 0, Length: window}, {Offset: 1073741824, Length: window}, {Offset: 10737418240, Length: window}, {Offset: 107374182400, Length: window}, {Offset: 1073741824000, Length: window}, {Offset: 2000264716288, Length: window}}, 805306368},
		{16000900661248, []EraseRegion{{Offset: 0, Length: window}, {Offset: 1073741824, Length: window}, {Offset: 10737418240, Length: window}, {Offset: 107374182400, Length: window}, {Offset: 1073741824000, Length: window}, {Offset: 16000766443520, Length: window}}, 805306368},
	} {
		got := MetadataClearRegions(test.size)
		if !slices.Equal(got, test.regions) {
			t.Fatalf("size %d: regions %v, want %v", test.size, got, test.regions)
		}
		for _, region := range got {
			if region.Offset%4096 != 0 || region.Length%4096 != 0 || region.Length <= 0 || region.Offset+region.Length > test.size {
				t.Fatalf("size %d: region %v is unaligned or outside the disk", test.size, region)
			}
		}
		target := EraseTarget{Method: EraseMetadataClear, SizeBytes: test.size}
		if covered := EraseCoverageBytes(target); covered != test.covered {
			t.Fatalf("size %d: metadataClear covers %d, want %d", test.size, covered, test.covered)
		}
		target.Method = EraseLogicalOverwrite
		if covered := EraseCoverageBytes(target); covered != test.size {
			t.Fatalf("size %d: logicalOverwrite covers %d", test.size, covered)
		}
		target.Method = ""
		if covered := EraseCoverageBytes(target); covered != 0 {
			t.Fatalf("an unresolved method covers %d bytes", covered)
		}
	}
}
