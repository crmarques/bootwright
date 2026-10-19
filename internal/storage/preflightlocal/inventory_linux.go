package preflightlocal

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

const domainProofBound = 1 << 20

func foreignCPUs(ctx context.Context, allocations []preflight.Allocation) (int64, error) {
	domains := map[string]domainXML{}
	for _, directory := range []string{"/etc/libvirt/qemu", "/run/libvirt/qemu"} {
		if err := readDomainDirectory(ctx, directory, readRootProof, domains); err != nil {
			return 0, err
		}
	}
	return foreignBudget(domains, allocations)
}
func readDomainDirectory(ctx context.Context, directory string, read func(string, int64) ([]byte, error), domains map[string]domainXML) error {
	file, err := openSafe(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := file.ReadDir(1025)
	file.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > 1024 {
		return errors.New("libvirt inventory exceeds its domain bound")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasSuffix(entry.Name(), ".xml") || entry.IsDir() {
			continue
		}
		data, err := read(directory+"/"+entry.Name(), domainProofBound)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		domain, err := inventoryDomain(data)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if old, found := domains[domain.UUID]; found {
			if old.Name != domain.Name {
				return fmt.Errorf("%s: libvirt inventory contradicts domain identity", entry.Name())
			}
			domain.VCPU = max(old.VCPU, domain.VCPU)
		}
		domains[domain.UUID] = domain
	}
	return nil
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
