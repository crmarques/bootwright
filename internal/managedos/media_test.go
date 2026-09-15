package managedos

import (
	"strings"
	"testing"
)

func TestMediaNamesAdmitOnlyPortableISOBasenames(t *testing.T) {
	for name, want := range map[string]bool{
		"a.iso":                           true,
		"rhel-9.7-x86_64-dvd.iso":         true,
		"RHEL_9.iso":                      true,
		"9.iso":                           true,
		".iso":                            false,
		"iso":                             false,
		"a.ISO":                           false,
		"-lead.iso":                       false,
		"trail-.iso":                      false,
		"../escape.iso":                   false,
		"dir/image.iso":                   false,
		"with space.iso":                  false,
		"con.iso":                         false,
		"COM4.iso":                        false,
		"lpt9.iso":                        false,
		"com10.iso":                       true,
		strings.Repeat("a", 251) + ".iso": true,
		strings.Repeat("a", 252) + ".iso": false,
	} {
		if got := ValidMediaName(name); got != want {
			t.Errorf("ValidMediaName(%q) = %t, want %t", name, got, want)
		}
	}
}

func TestMediaDigestsNormalizeToLowercaseHexadecimal(t *testing.T) {
	digest := strings.Repeat("AB", 32)
	value, ok := NormalizeMediaDigest("sha256:" + digest)
	if !ok || value != strings.ToLower(digest) {
		t.Fatalf("normalized = %q ok=%t", value, ok)
	}
	if value, ok := NormalizeMediaDigest("  "); !ok || value != "" {
		t.Fatalf("an absent digest is not an error: %q %t", value, ok)
	}
	for _, invalid := range []string{"sha512:" + digest, digest[:63], digest + "a", strings.Repeat("g", 64)} {
		if _, ok := NormalizeMediaDigest(invalid); ok {
			t.Errorf("NormalizeMediaDigest(%q) was accepted", invalid)
		}
	}
}

func validEntry() MediaEntry {
	return MediaEntry{
		Name: "rhel-9.7-x86_64-boot.iso", Size: 1388429312, SHA256: strings.Repeat("a", 64),
		Source: "file:///images/rhel-9.7-x86_64-boot.iso", Added: "2026-09-15T09:00:00Z",
	}
}

func TestMediaRecordsRoundTripExactlyOneImage(t *testing.T) {
	entry := validEntry()
	data, err := EncodeMediaRecord(entry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMediaRecord(data, entry.Name)
	if err != nil || decoded != entry {
		t.Fatalf("decoded = %+v (%v)", decoded, err)
	}
	if _, err := DecodeMediaRecord(data, "other.iso"); err == nil {
		t.Fatal("a record was accepted beside another image")
	}
}

func TestMediaRecordsRefuseNoncanonicalAndUnprovableValues(t *testing.T) {
	entry := validEntry()
	canonical, err := EncodeMediaRecord(entry)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"no trailing newline": canonical[:len(canonical)-1],
		"reordered fields":    []byte(`{"name":"rhel-9.7-x86_64-boot.iso","version":1,"size":1388429312,"sha256":"` + strings.Repeat("a", 64) + `","source":"file:///x","added":"2026-09-15T09:00:00Z"}` + "\n"),
		"unknown field":       append(append([]byte{}, canonical[:len(canonical)-2]...), []byte(`,"extra":1}`+"\n")...),
		"wrong version":       []byte(strings.Replace(string(canonical), `"version":1`, `"version":2`, 1)),
	} {
		if _, err := DecodeMediaRecord(data, entry.Name); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for name, broken := range map[string]MediaEntry{
		"absent digest":    {Name: entry.Name, Size: 1, Source: "x", Added: "y"},
		"negative size":    {Name: entry.Name, Size: -1, SHA256: entry.SHA256, Source: "x", Added: "y"},
		"invalid name":     {Name: "bad name.iso", Size: 1, SHA256: entry.SHA256, Source: "x", Added: "y"},
		"absent origin":    {Name: entry.Name, Size: 1, SHA256: entry.SHA256, Added: "y"},
		"control in text":  {Name: entry.Name, Size: 1, SHA256: entry.SHA256, Source: "x\ny", Added: "y"},
		"uppercase digest": {Name: entry.Name, Size: 1, SHA256: strings.ToUpper(entry.SHA256), Source: "x", Added: "y"},
	} {
		if _, err := EncodeMediaRecord(broken); err == nil {
			t.Errorf("%s was encoded", name)
		}
	}
}

func TestMediaReservationKeyNamesItsImage(t *testing.T) {
	if got := MediaReservationKey("rhel.iso"); got != "media:rhel.iso" {
		t.Fatalf("key = %q", got)
	}
}
