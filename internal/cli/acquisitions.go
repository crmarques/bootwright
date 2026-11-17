package cli

import (
	"errors"
	"io"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type Acquisition struct {
	Kind     string
	Artifact string
	Version  string
	Origin   string
	Class    string
	SHA256   string
	Bytes    int64
	Machines []string
	Consumer string
}

type AcquisitionList func(api.Catalog) ([]Acquisition, error)

type acquisitionPresentation struct {
	Kind     string   `json:"kind"`
	Artifact string   `json:"artifact"`
	Version  string   `json:"version"`
	Origin   string   `json:"origin"`
	Class    string   `json:"class"`
	SHA256   string   `json:"sha256"`
	Bytes    int64    `json:"bytes"`
	Machines []string `json:"machines"`
	Consumer string   `json:"consumer"`
}

type acquisitionsPresentation struct {
	Acquisitions []acquisitionPresentation `json:"acquisitions"`
}

func (acquisitionsPresentation) documentedResult() {}

func acquisitionsCommand() commandSpec {
	return available(commandSpec{path: "render acquisitions", results: []jsonResult{acquisitionsPresentation{}}, render: renderAcquisitions(), validate: validateRenderAcquisitions, classify: classifyRules{localInput: hasInputDir}, example: "--input-dir ./input", invoke: Services.invokeRenderAcquisitions, short: "List every artifact the selected desired state acquires",
		long: "List every artifact the selected desired state acquires or hands a Machine to acquire: the host packages and clients of the controller stage, each managed service's and the emulated BMC's image, each container cluster's release, declared mirrors, default operator catalogs and update service, each add-on binding's catalog image, each registry's token and delivery hosts, each storage cluster's images and package repository, each installed Machine's own package repositories, and the installation media of the media store." +
			" Each row names the artifact, its version, its origin and that origin's class (vendor, vendor delivery, public, declared, host repositories or media store), its SHA-256 and size where the build or the desired state pins them, the Machines that read it and the object that selects it." +
			" It reads the selected context's input, or with --input-dir a context-free input file or directory, registers nothing and contacts nothing.",
		flags: []flagSpec{directoryFlag("input-dir", "Read a context-free input file or directory instead of a context")}})
}

func writeAcquisitions(out io.Writer, command string, list AcquisitionList, catalog api.Catalog, structured bool) error {
	if list == nil {
		return errors.New("this executable lists no acquisitions")
	}
	acquired, err := list(catalog)
	if err != nil {
		return err
	}
	acquired = slices.Clone(acquired)
	slices.SortStableFunc(acquired, func(a, b Acquisition) int {
		return strings.Compare(escapeDisplayLine(a.Kind)+"\x00"+escapeDisplayLine(a.Artifact)+"\x00"+escapeDisplayLine(a.Consumer),
			escapeDisplayLine(b.Kind)+"\x00"+escapeDisplayLine(b.Artifact)+"\x00"+escapeDisplayLine(b.Consumer))
	})
	shown := acquisitionsPresentation{Acquisitions: []acquisitionPresentation{}}
	rows := make([][]string, 0, len(acquired))
	for _, item := range acquired {
		machines := slices.Clone(item.Machines)
		slices.Sort(machines)
		machines = slices.Compact(machines)
		for index := range machines {
			machines[index] = escapeDisplayLine(machines[index])
		}
		if machines == nil {
			machines = []string{}
		}
		shown.Acquisitions = append(shown.Acquisitions, acquisitionPresentation{Kind: escapeDisplayLine(item.Kind), Artifact: escapeDisplayLine(item.Artifact), Version: escapeDisplayLine(item.Version),
			Origin: escapeDisplayLine(item.Origin), Class: escapeDisplayLine(item.Class), SHA256: escapeDisplayLine(item.SHA256), Bytes: item.Bytes, Machines: machines, Consumer: escapeDisplayLine(item.Consumer)})
		size := "none"
		if item.Bytes > 0 {
			size = humanBytes(item.Bytes)
		}
		raw := slices.Compact(slices.Sorted(slices.Values(item.Machines)))
		rows = append(rows, []string{item.Kind, item.Artifact, textOrNone(item.Version), item.Class, textOrNone(item.Origin),
			textOrNone(item.SHA256), size, joinedOrNone(raw), textOrNone(item.Consumer)})
	}
	if structured {
		return writeStructuredResult(out, command, shown, []diagnostic{}, []string{}, 0)
	}
	var text display
	text.resourceTable([]string{"KIND", "ARTIFACT", "VERSION", "CLASS", "ORIGIN", "SHA-256", "SIZE", "MACHINES", "SELECTED BY"}, rows, tableStyle{Kind: "acquisition", Identity: 1})
	return text.writeTo(out)
}
