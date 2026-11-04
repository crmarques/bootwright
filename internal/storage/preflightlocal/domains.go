package preflightlocal

import (
	"encoding/xml"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

const domainProofBound = 1 << 20

type domainXML struct {
	Name  string `xml:"name"`
	UUID  string `xml:"uuid"`
	VCPU  int64  `xml:"vcpu"`
	Disks []struct {
		Type   string `xml:"type,attr"`
		Device string `xml:"device,attr"`
		Source struct {
			File string `xml:"file,attr"`
		} `xml:"source"`
	} `xml:"devices>disk"`
}

func parseMemory(data []byte) (int64, bool) { return parseMemoryKey(data, "MemAvailable:") }
func parseMemoryKey(data []byte, key string) (int64, bool) {
	var available int64
	found := false
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != key {
			continue
		}
		if found || len(fields) != 3 || fields[2] != "kB" {
			return 0, false
		}
		number, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || number < 0 || number > math.MaxInt64/1024 {
			return 0, false
		}
		available = number * 1024
		found = true
	}
	return available, found
}
func inventoryReason(err error) string {
	cause := err.Error()
	if len(cause) > 160 {
		cause = strings.ToValidUTF8(cause[:160], "")
	}
	return "the libvirt domain inventory could not be read: " + cause
}
func inventoryDomain(data []byte) (domainXML, error) {
	var value struct {
		XMLName xml.Name
		domainXML
		Domain domainXML `xml:"domain"`
	}
	if err := xml.Unmarshal(data, &value); err != nil {
		return domainXML{}, err
	}
	domain := value.domainXML
	switch value.XMLName.Local {
	case "domain":
	case "domstatus":
		domain = value.Domain
	default:
		return domainXML{}, errors.New("unrecognized libvirt domain inventory")
	}
	if domain.Name == "" || domain.UUID == "" || domain.VCPU <= 0 {
		return domainXML{}, errors.New("libvirt inventory omits domain identity or CPU budget")
	}
	return domain, nil
}
func foreignBudget(domains map[string]domainXML, allocations []preflight.Allocation) (int64, error) {
	wanted := map[string]string{}
	for _, allocation := range allocations {
		if allocation.UUID == "" || allocation.Domain == "" {
			return 0, errors.New("projected domain identity is missing")
		}
		wanted[allocation.UUID] = allocation.Domain
	}
	total := int64(0)
	for uuid, domain := range domains {
		if name, found := wanted[uuid]; found {
			if name != domain.Name {
				return 0, errors.New("a foreign domain holds the projected UUID")
			}
			continue
		}
		if domain.VCPU <= 0 || total > math.MaxInt64-domain.VCPU {
			return 0, errors.New("foreign CPU budget is invalid or overflows")
		}
		total += domain.VCPU
	}
	return total, nil
}
func mergeDomain(domains map[string]domainXML, data []byte) error {
	domain, err := inventoryDomain(data)
	if err != nil {
		return err
	}
	if old, found := domains[domain.UUID]; found {
		if old.Name != domain.Name {
			return errors.New("libvirt inventory contradicts domain identity")
		}
		domain.VCPU = max(old.VCPU, domain.VCPU)
	}
	domains[domain.UUID] = domain
	return nil
}
