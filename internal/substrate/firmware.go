package substrate

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	FirmwareModeEnforce = "enforce"
	FirmwareModeProve   = "prove"

	FirmwareAttributes  = 64
	FirmwareNameBytes   = 128
	FirmwareStringBytes = 256

	FirmwarePath = "spec.substrate." + ArmBaremetal + ".firmware"
)

type Firmware struct {
	BIOSAttributes []BIOSAttribute `json:"biosAttributes,omitempty"`
	BootMode       string          `json:"bootMode,omitempty"`
	Mode           string          `json:"mode"`
	SecureBoot     *bool           `json:"secureBoot,omitempty"`
}

type BIOSAttribute struct {
	Boolean *bool   `json:"boolean,omitempty"`
	Integer *int64  `json:"integer,omitempty"`
	Name    string  `json:"name"`
	String  *string `json:"string,omitempty"`
}

func MachineFirmware(machine api.Object) *Firmware {
	block := machine.Spec().Get("substrate", ArmBaremetal, "firmware")
	if !block.Present() {
		return nil
	}
	firmware := &Firmware{Mode: block.Get("mode").Text(), BootMode: block.Get("bootMode").Text()}
	if firmware.Mode == "" {
		firmware.Mode = FirmwareModeEnforce
	}
	if block.Has("secureBoot") {
		enabled := block.Get("secureBoot").Bool()
		firmware.SecureBoot = &enabled
	}
	for _, entry := range block.Get("biosAttributes").Fields() {
		attribute := BIOSAttribute{Name: entry.Name}
		switch entry.Value.Type() {
		case api.String:
			text := entry.Value.Text()
			attribute.String = &text
		case api.Integer:
			number, ok := entry.Value.Int64()
			if !ok {
				continue
			}
			attribute.Integer = &number
		case api.Boolean:
			flag := entry.Value.Bool()
			attribute.Boolean = &flag
		default:
			continue
		}
		firmware.BIOSAttributes = append(firmware.BIOSAttributes, attribute)
	}
	slices.SortFunc(firmware.BIOSAttributes, func(a, b BIOSAttribute) int { return strings.Compare(a.Name, b.Name) })
	return firmware
}
