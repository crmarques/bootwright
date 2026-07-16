package safefs

import (
	"strings"
	"testing"
)

func TestMountpointsUnderOrdersChildrenFirst(t *testing.T) {
	mountinfo := strings.Join([]string{
		"25 1 8:1 / / rw,relatime - xfs /dev/sda1 rw",
		"90 25 7:0 / /var/lib/bootwright/contexts/prd/provider-state/os-install/ceph-prd/srv4203/tree-mnt ro,relatime - iso9660 /dev/loop0 ro",
		"91 25 7:1 / /var/lib/bootwright/contexts/prd/provider-state/os-install ro,relatime - iso9660 /dev/loop1 ro",
		"92 25 8:2 / /var/lib/elsewhere rw,relatime - xfs /dev/sda2 rw",
	}, "\n")
	mounts, err := mountpointsUnder(strings.NewReader(mountinfo), "/var/lib/bootwright/contexts/prd")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/var/lib/bootwright/contexts/prd/provider-state/os-install/ceph-prd/srv4203/tree-mnt",
		"/var/lib/bootwright/contexts/prd/provider-state/os-install",
	}
	if len(mounts) != len(want) {
		t.Fatalf("got %v, want %v", mounts, want)
	}
	for i := range want {
		if mounts[i] != want[i] {
			t.Fatalf("got %v, want %v", mounts, want)
		}
	}
}

func TestMountpointsUnderStackedMountsLastMountedFirst(t *testing.T) {
	mountinfo := strings.Join([]string{
		"90 25 7:0 / /data/ctx/mnt ro,relatime - iso9660 /dev/loop0 ro",
		"91 25 7:1 / /data/ctx/mnt ro,relatime - iso9660 /dev/loop1 ro",
	}, "\n")
	mounts, err := mountpointsUnder(strings.NewReader(mountinfo), "/data/ctx")
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 {
		t.Fatalf("got %v, want two entries", mounts)
	}
}

func TestMountpointsUnderExcludesSiblingPrefix(t *testing.T) {
	mountinfo := "90 25 7:0 / /data/ctx-other/mnt ro,relatime - iso9660 /dev/loop0 ro"
	mounts, err := mountpointsUnder(strings.NewReader(mountinfo), "/data/ctx")
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 0 {
		t.Fatalf("got %v, want none", mounts)
	}
}

func TestMountpointsUnderMatchesDirItself(t *testing.T) {
	mountinfo := "90 25 7:0 / /data/ctx ro,relatime - iso9660 /dev/loop0 ro"
	mounts, err := mountpointsUnder(strings.NewReader(mountinfo), "/data/ctx")
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0] != "/data/ctx" {
		t.Fatalf("got %v, want [/data/ctx]", mounts)
	}
}

func TestUnescapeMountPath(t *testing.T) {
	cases := map[string]string{
		`/data/with\040space`:      "/data/with space",
		`/data/back\134slash`:      `/data/back\slash`,
		`/data/plain`:              "/data/plain",
		`/data/trailing\04`:        `/data/trailing\04`,
		`/data/tab\011newline\012`: "/data/tab\tnewline\n",
	}
	for in, want := range cases {
		if got := unescapeMountPath(in); got != want {
			t.Fatalf("unescapeMountPath(%q) = %q, want %q", in, got, want)
		}
	}
}
