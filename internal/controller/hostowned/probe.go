// Package hostowned proves for controller uninstall that no Bootwright-owned host object remains.
package hostowned

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	virshPath        = "/usr/bin/virsh"
	systemctlPath    = "/usr/bin/systemctl"
	podmanPath       = "/usr/bin/podman"
	libvirtRuntime   = "/run/libvirt"
	quadletDirectory = "/etc/containers/systemd"
	connection       = "qemu:///system"
	maxObjects       = 4096
	maxName          = 256
	uninstallCode    = "controller.uninstall"
	uninstallAgain   = "then run bootwright controller uninstall again"
)

type Config struct {
	Prefix      string
	Namespace   string
	ImagePrefix string
}

type result struct {
	stdout []byte
	stderr string
	code   int
}

type runner func(ctx context.Context, path string, args []string) (result, error)

type Probe struct {
	config  Config
	run     runner
	qualify func(string) (string, error)
	sockets func(string) (bool, error)
	entries func(string) ([]string, error)
}

var _ prerequisites.HostOwnedObjects = (*Probe)(nil)

func (p *Probe) OwnedObjects(ctx context.Context) ([]prerequisites.OwnedObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || p.run == nil || p.qualify == nil || p.sockets == nil || p.entries == nil || p.config.Prefix == "" || p.config.Namespace == "" || p.config.ImagePrefix == "" {
		return nil, diagnostics.NewFailureWithRemediation(uninstallCode, "the owned-object proof is not configured", "", "")
	}
	owned := []prerequisites.OwnedObject{}
	for _, probe := range []func(context.Context) ([]prerequisites.OwnedObject, error){p.libvirt, p.units, p.quadlets, p.containers} {
		found, err := probe(ctx)
		if err != nil {
			return nil, err
		}
		owned = append(owned, found...)
	}
	slices.SortFunc(owned, func(a, b prerequisites.OwnedObject) int {
		return strings.Compare(a.Kind+" "+a.Name, b.Kind+" "+b.Name)
	})
	return slices.Compact(owned), nil
}

func (p *Probe) tool(path string) (string, bool, error) {
	resolved, err := p.qualify(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", true, diagnostics.NewFailureWithRemediation(uninstallCode, path+" is not a root-owned system executable", "",
			"restore "+path+" with root ownership and no group or other write access, "+uninstallAgain)
	}
	return resolved, true, nil
}

func unproved(what, command, detail string) error {
	return diagnostics.NewFailureWithRemediation(uninstallCode,
		"uninstall cannot prove that no Bootwright-owned "+what+" remains: "+detail, "",
		"make "+command+" answer as root, "+uninstallAgain)
}

func (p *Probe) output(ctx context.Context, what, tool string, args ...string) ([]byte, error) {
	return p.listing(ctx, what, tool, 0, args...)
}

func (p *Probe) listing(ctx context.Context, what, tool string, silentNone int, args ...string) ([]byte, error) {
	command := strings.Join(append([]string{tool}, args...), " ")
	answer, err := p.run(ctx, tool, args)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, unproved(what, command, command+" could not run")
	}
	if answer.code != 0 && answer.code == silentNone && len(answer.stdout) == 0 && answer.stderr == "" {
		return nil, nil
	}
	if answer.code != 0 {
		detail := command + " exited " + strconv.Itoa(answer.code)
		if text := printable(answer.stderr); text != "" {
			detail += ": " + text
		}
		return nil, unproved(what, command, detail)
	}
	return answer.stdout, nil
}

func (p *Probe) names(ctx context.Context, what, tool string, args ...string) ([]string, error) {
	data, err := p.output(ctx, what, tool, args...)
	if err != nil {
		return nil, err
	}
	names, ok := parseNames(data)
	if !ok {
		return nil, unproved(what, strings.Join(append([]string{tool}, args...), " "), "its output is not a list of names")
	}
	return names, nil
}

func parseNames(data []byte) ([]string, bool) {
	names := []string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if !validName(name) || len(names) == maxObjects {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

func validName(name string) bool {
	return len(name) <= maxName && utf8.ValidString(name) && !strings.ContainsFunc(name, func(r rune) bool { return !unicode.IsPrint(r) })
}

func printable(text string) string {
	text = strings.Join(strings.Fields(strings.ToValidUTF8(text, "?")), " ")
	text = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return '?'
	}, text)
	if len(text) > 512 {
		text = text[:512]
	}
	return text
}

type libvirtKind struct {
	kind, list, dump string
}

func (p *Probe) libvirt(ctx context.Context) ([]prerequisites.OwnedObject, error) {
	virsh, present, err := p.tool(virshPath)
	if err != nil {
		return nil, err
	}
	if !present {
		socket, err := p.sockets(libvirtRuntime)
		if err != nil {
			return nil, unproved("libvirt object", "the listing of "+libvirtRuntime, libvirtRuntime+" cannot be inspected")
		}
		if socket {
			return nil, unproved("libvirt object", virshPath, "a libvirt socket exists under "+libvirtRuntime+" but "+virshPath+" is missing")
		}
		return nil, nil
	}
	owned := []prerequisites.OwnedObject{}
	for _, kind := range []libvirtKind{{"libvirt domain", "list", "dumpxml"}, {"libvirt network", "net-list", "net-dumpxml"}, {"libvirt pool", "pool-list", "pool-dumpxml"}} {
		names, err := p.names(ctx, kind.kind, virsh, "-c", connection, "-q", kind.list, "--all", "--name")
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			marked := strings.HasPrefix(name, p.config.Prefix)
			if !marked {
				document, err := p.output(ctx, kind.kind, virsh, "-c", connection, "-q", kind.dump, "--inactive", "--", name)
				if err != nil {
					return nil, err
				}
				var ok bool
				if marked, ok = p.markedDocument(document, kind.kind == "libvirt pool"); !ok {
					return nil, unproved(kind.kind, virsh+" "+kind.dump+" "+name, "its XML cannot be parsed")
				}
			}
			if marked {
				owned = append(owned, prerequisites.OwnedObject{Kind: kind.kind, Name: name})
			}
		}
	}
	return owned, nil
}

func (p *Probe) markedDocument(document []byte, pool bool) (marked, ok bool) {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	var path []string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return marked, len(path) == 0
		}
		if err != nil {
			return false, false
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Space == p.config.Namespace {
				marked = true
			}
			path = append(path, element.Name.Local)
		case xml.EndElement:
			if len(path) == 0 {
				return false, false
			}
			path = path[:len(path)-1]
		case xml.CharData:
			target := strings.TrimSpace(string(element))
			if pool && slices.Equal(path, []string{"pool", "target", "path"}) &&
				(target == p.config.ImagePrefix || strings.HasPrefix(target, p.config.ImagePrefix+"/")) {
				marked = true
			}
		}
	}
}

func (p *Probe) units(ctx context.Context) ([]prerequisites.OwnedObject, error) {
	systemctl, present, err := p.tool(systemctlPath)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, unproved("systemd unit", systemctlPath, systemctlPath+" is missing")
	}
	pattern := p.config.Prefix + "*"
	owned := []prerequisites.OwnedObject{}
	for _, listing := range []struct {
		silentNone int
		args       []string
	}{
		{0, []string{"list-units", "--all", "--plain", "--no-legend", "--no-pager", "--full", "--", pattern}},
		{1, []string{"list-unit-files", "--no-legend", "--no-pager", "--full", "--", pattern}},
	} {
		args := listing.args
		data, err := p.listing(ctx, "systemd unit", systemctl, listing.silentNone, args...)
		if err != nil {
			return nil, err
		}
		units, ok := parseUnits(data, p.config.Prefix)
		if !ok {
			return nil, unproved("systemd unit", systemctl+" "+args[0], "its output is not a list of units")
		}
		for _, unit := range units {
			owned = append(owned, prerequisites.OwnedObject{Kind: "systemd unit", Name: unit})
		}
	}
	return owned, nil
}

func parseUnits(data []byte, prefix string) ([]string, bool) {
	units := []string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := fields[0]
		if !validName(unit) || !strings.HasPrefix(unit, prefix) || !strings.Contains(unit, ".") || len(units) == maxObjects {
			return nil, false
		}
		units = append(units, unit)
	}
	return units, true
}

func (p *Probe) quadlets(context.Context) ([]prerequisites.OwnedObject, error) {
	names, err := p.entries(quadletDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, unproved("quadlet source", "the listing of "+quadletDirectory, quadletDirectory+" cannot be listed")
	}
	owned := []prerequisites.OwnedObject{}
	for _, name := range names {
		if strings.HasPrefix(name, p.config.Prefix) && strings.HasSuffix(name, ".container") {
			owned = append(owned, prerequisites.OwnedObject{Kind: "quadlet source", Name: quadletDirectory + "/" + name})
		}
	}
	return owned, nil
}

func (p *Probe) containers(ctx context.Context) ([]prerequisites.OwnedObject, error) {
	podman, present, err := p.tool(podmanPath)
	if err != nil || !present {
		return nil, err
	}
	names, err := p.names(ctx, "podman container", podman, "ps", "--all", "--format", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	owned := []prerequisites.OwnedObject{}
	for _, name := range names {
		if strings.HasPrefix(name, p.config.Prefix) {
			owned = append(owned, prerequisites.OwnedObject{Kind: "podman container", Name: name})
		}
	}
	return owned, nil
}
