package inventory

import "testing"

// TestNormalizeRedfishURL pins how vendor BMC addresses get shaped
// before they reach boot_redfish / external_validate. The role
// appends "/redfish/v1/Systems[/...]" to baseUrl unconditionally,
// so any Ironic-style transport hint and any trailing /Systems/<id>
// must be stripped here — otherwise the doubled-path silently 404s
// inside `wait-for install-complete` 30 minutes later. systemId is
// returned alongside so the role can target the embedded /Systems/<id>
// directly without an extra discovery round-trip.
func TestNormalizeRedfishURL(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantBase     string
		wantSystemID string
	}{
		{
			name:         "ironic_virtualmedia_with_system_id",
			in:           "redfish-virtualmedia+http://192.168.132.1:8000/redfish/v1/Systems/7ec5d09a-2be9-5fc1-8505-0dea5887aa8a",
			wantBase:     "http://192.168.132.1:8000",
			wantSystemID: "7ec5d09a-2be9-5fc1-8505-0dea5887aa8a",
		},
		{
			name:         "ironic_https_with_system_id",
			in:           "redfish+https://bmc.example.com/redfish/v1/Systems/Self",
			wantBase:     "https://bmc.example.com",
			wantSystemID: "Self",
		},
		{
			name:         "ironic_virtualmedia_https_with_system_id",
			in:           "redfish-virtualmedia+https://10.1.255.61/redfish/v1/Systems/1",
			wantBase:     "https://10.1.255.61",
			wantSystemID: "1",
		},
		{
			name:         "virtualmedia_default_https_with_system_id",
			in:           "redfish-virtualmedia://bmc.example.com/redfish/v1/Systems/1",
			wantBase:     "https://bmc.example.com",
			wantSystemID: "1",
		},
		{
			name:         "virtualmedia_default_https_base",
			in:           "redfish-virtualmedia://bmc.example.com",
			wantBase:     "https://bmc.example.com",
			wantSystemID: "",
		},
		{
			name:         "redfish_default_https_base",
			in:           "redfish://bmc.example.com",
			wantBase:     "https://bmc.example.com",
			wantSystemID: "",
		},
		{
			name:         "plain_redfish_base",
			in:           "https://bmc.vendor.test/",
			wantBase:     "https://bmc.vendor.test",
			wantSystemID: "",
		},
		{
			name:         "operator_placeholder_passthrough",
			in:           "REPLACE_WITH_REAL_BMC_URL_FOR_master-0",
			wantBase:     "REPLACE_WITH_REAL_BMC_URL_FOR_master-0",
			wantSystemID: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, sysID := normalizeRedfishURL(tc.in)
			if base != tc.wantBase {
				t.Errorf("base got %q, want %q", base, tc.wantBase)
			}
			if sysID != tc.wantSystemID {
				t.Errorf("systemID got %q, want %q", sysID, tc.wantSystemID)
			}
		})
	}
}
