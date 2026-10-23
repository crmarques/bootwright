package firewalllocal

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"time"
)

type capture struct{ bytes.Buffer }

func (c *capture) Write(data []byte) (int, error) {
	if c.Len()+len(data) > 65536 {
		return 0, errors.New("firewall query output exceeded its bound")
	}
	return c.Buffer.Write(data)
}

func command(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/firewall-cmd", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	output, failures := &capture{}, &capture{}
	cmd.Stdout, cmd.Stderr = output, failures
	if err := cmd.Run(); err != nil {
		var exited *exec.ExitError
		if !errors.As(err, &exited) || !qualifiedNegative(args, output.String(), failures.String(), exited.ExitCode()) {
			return nil, errors.New("read-only runtime firewall query did not complete")
		}
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

func qualifiedNegative(args []string, output, failure string, exitCode int) bool {
	if len(args) != 1 {
		return false
	}
	if args[0] == "--query-panic" {
		return exitCode == 1 && output == "no\n" && failure == ""
	}
	return strings.HasPrefix(args[0], "--get-zone-of-interface=") && exitCode == 2 && output == "" && failure == "no zone\n"
}
