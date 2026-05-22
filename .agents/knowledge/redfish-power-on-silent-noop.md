# Redfish Reset(On) reports success but the VM stays off

**Symptom:** The openshift-install agent log loops on
`Failed to connect to the Rendezvous Host on port 22: ... no route to host`
and `Cannot access Rendezvous Host`. The boot_redfish play completed
without error, sushy-emulator's access log shows `204` for
`ComputerSystem.Reset`, yet `virsh list` reports the target domain as
`shut off`.

**Root cause:** Sushy-emulator's libvirt backend implements
`set_power_state('On')` as `domain.create()` and returns success
whenever that call does not raise. Races where libvirt accepts the
start request and qemu exits within milliseconds (BIOS-level boot
failure, on_crash=destroy, transient SELinux/DAC denial on a freshly
inserted vmedia file) are invisible to the BMC client — Reset returns
204 and install_agent enters `wait-for install-complete`, waiting
forever for an agent that was never running.

**Fix:** After Reset(On), poll the System resource until
`PowerState == On` before handing off to wait-for. boot_redfish runs
this poll automatically using
`bootwright_redfish_power_state_retries` /
`bootwright_redfish_power_state_delay_seconds`.

