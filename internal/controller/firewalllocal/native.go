package firewalllocal

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/nativeexec"
)

const outputBound = 65536

func command(ctx context.Context, args []string) ([]byte, error) {
	executable, err := nativeexec.Qualify("/usr/bin/firewall-cmd")
	if err != nil {
		return nil, errors.New("the system executable is not root-owned below qualified directories")
	}
	output, failures := nativeexec.Head(outputBound), nativeexec.Head(outputBound)
	result := nativeexec.Run(ctx, nativeexec.Command{
		Path: executable, Args: args, Env: []string{"PATH=/usr/bin:/bin", "LC_ALL=C"},
		Stdout: output, Stderr: failures, WaitDelay: time.Second,
	})
	complete := result.Outcome == nativeexec.Exited && result.Err == nil && !output.Overflowed() && !failures.Overflowed()
	if !complete || result.ExitCode != 0 && !qualifiedNegative(args, output.String(), failures.String(), result.ExitCode) {
		return nil, errors.New("read-only runtime firewall query did not complete")
	}
	return output.Bytes(), nil
}

func localInterfaces() ([]Interface, error) {
	links, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	if len(links) > 1024 {
		return nil, errors.New("controller interface inventory exceeded its bound")
	}
	result := make([]Interface, 0, len(links))
	for _, link := range links {
		addresses, err := link.Addrs()
		if err != nil || len(addresses) > 1024 {
			return nil, errors.New("controller addresses could not be bounded")
		}
		current := Interface{Name: link.Name}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil {
				return nil, err
			}
			current.Addresses = append(current.Addresses, prefix.Addr())
		}
		result = append(result, current)
	}
	return result, nil
}

func lookupIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func qualifiedNegative(args []string, output, failure string, exitCode int) bool {
	if len(args) != 1 {
		return false
	}
	if args[0] == "--query-panic" {
		return exitCode == 1 && output == "no\n" && failure == ""
	}
	return strings.HasPrefix(args[0], "--get-zone-of-interface=") && exitCode == 2 && output == "" && failure == "no zone\n"
}
