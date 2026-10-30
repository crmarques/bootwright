package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"strconv"
	"strings"
)

const bootstrapConnections = "/run/NetworkManager/system-connections"

func encryptionNetworkLines(input Installation) []string {
	if input.DiskEncryption == nil {
		return nil
	}
	var profiles [][]string
	var activate []string
	add := func(name, kind, master string, settings []string) {
		profile := []string{"[connection]", "id=" + name, "uuid=" + bootstrapUUID(input.Hostname, name),
			"type=" + kind, "interface-name=" + name, "autoconnect=false"}
		if master != "" {
			profile = append(profile, "master="+bootstrapUUID(input.Hostname, master), "slave-type=bond")
		}
		if kind == "bond" {
			profile = append(profile, "autoconnect-slaves=1")
		}
		profile = append(profile, settings...)
		if name == input.Interface {
			profile = append(profile, "", "[ipv4]", "method=manual", "address1="+input.Address+"/"+strconv.Itoa(input.Prefix))
			if input.Gateway != "" {
				profile = append(profile, "gateway="+input.Gateway)
			}
			var ipv4Resolvers, ipv6Resolvers []string
			for _, server := range input.Nameservers {
				if address, err := netip.ParseAddr(server); err == nil {
					if address.Is4() {
						ipv4Resolvers = append(ipv4Resolvers, server)
					} else {
						ipv6Resolvers = append(ipv6Resolvers, server)
					}
				}
			}
			if len(ipv4Resolvers) != 0 {
				profile = append(profile, "dns="+strings.Join(ipv4Resolvers, ";")+";")
			}
			profile = append(profile, "may-fail=false")
			profile = append(profile, "", "[ipv6]", "method=auto", "may-fail=true")
			if len(ipv6Resolvers) != 0 {
				profile = append(profile, "dns="+strings.Join(ipv6Resolvers, ";")+";")
			}
		} else if master == "" {
			profile = append(profile, "", "[ipv4]", "method=disabled", "", "[ipv6]", "method=disabled")
		}
		profiles = append(profiles, profile)
		if master == "" {
			activate = append(activate, bootstrapUUID(input.Hostname, name))
		}
	}
	base := input.Interface
	if input.InterfaceType == "vlan" {
		base = input.VLANBase
	}
	if len(input.BondPorts) != 0 {
		for _, port := range input.BondPorts {
			add(port, "ethernet", base, nil)
		}
		add(base, "bond", "", append([]string{"", "[bond]"}, input.BondOptions...))
	} else {
		add(base, "ethernet", "", nil)
	}
	if input.InterfaceType == "vlan" {
		add(input.Interface, "vlan", "", []string{"", "[vlan]", "id=" + strconv.Itoa(input.VLANID),
			"parent=" + bootstrapUUID(input.Hostname, base)})
	}
	lines := []string{"mkdir -p " + bootstrapConnections, "chmod 0700 " + bootstrapConnections}
	var files []string
	for index, profile := range profiles {
		file := bootstrapConnections + "/bootwright-installer-" + strconv.Itoa(index) + ".nmconnection"
		files = append(files, file)
		lines = append(lines, "cat > "+file+" <<'BOOTWRIGHT_NETWORK_EOF'")
		lines = append(lines, profile...)
		lines = append(lines, "BOOTWRIGHT_NETWORK_EOF", "chmod 0600 "+file)
	}
	lines = append(lines, "nmcli --wait 60 connection load "+strings.Join(files, " "))
	for _, uuid := range activate {
		lines = append(lines, "nmcli --wait 60 connection up uuid "+uuid)
	}
	return lines
}

func bootstrapUUID(hostname, iface string) string {
	sum := sha256.Sum256([]byte("bootwright.managedos.installer-network\x00" + hostname + "\x00" + iface))
	sum[6] = sum[6]&0x0f | 0x80
	sum[8] = sum[8]&0x3f | 0x80
	value := hex.EncodeToString(sum[:16])
	return strings.Join([]string{value[:8], value[8:12], value[12:16], value[16:20], value[20:32]}, "-")
}
