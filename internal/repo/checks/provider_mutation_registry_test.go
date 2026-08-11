package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/roles"
)

type providerMutationSurface struct {
	name      string
	provider  string
	path      string
	gates     []string
	mutations []string
	evidence  []string
}

type providerMutationTask struct {
	path   string
	name   string
	action string
}

type providerMutationTaskRegistration struct {
	class   string
	surface string
	anchor  string
}

var providerMutationProviders = map[string]bool{
	"baremetal": true,
	"kubevirt":  true,
	"libvirt":   true,
	"vsphere":   true,
}

var providerMutationTasks = map[providerMutationTask]providerMutationTaskRegistration{
	{path: "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/destroy_machine.yml", name: "Dispatch selected machine substrate destroy", action: "ansible.builtin.include_role"}:                                                                                 {class: "removal", surface: "provider destroy dispatch", anchor: "Dispatch selected machine substrate destroy"},
	{path: "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/prepare_destroy_cluster.yml", name: "Detach managed load balancer VIPs", action: "ansible.builtin.import_role"}:                                                                                    {class: "removal", surface: "managed load balancer detach", anchor: "Detach managed load balancer VIPs"},
	{path: "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/prepare_destroy_cluster.yml", name: "Read libvirt network definition", action: "ansible.builtin.command"}:                                                                                          {class: "probe", surface: "libvirt network destroy", anchor: "Refuse to remove a non-Bootwright libvirt network"},
	{path: "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/remove_libvirt_network.yml", name: "Remove authorized libvirt network", action: "community.libvirt.virt_net"}:                                                                                      {class: "removal", surface: "libvirt network destroy", anchor: "Authorize cluster libvirt network removal"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Clone KubeVirt agent ISO DataVolume from the shared source", action: "ansible.builtin.command"}:                                                                    {class: "mutation", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Enforce KubeVirt agent ISO DataVolume apply mode", action: "ansible.builtin.include_role"}:                                                                         {class: "gate", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Enforce KubeVirt agent ISO source DataVolume apply mode", action: "ansible.builtin.include_role"}:                                                                  {class: "gate", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Label KubeVirt agent ISO DataVolume as Bootwright-managed", action: "ansible.builtin.command"}:                                                                     {class: "mutation", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Label KubeVirt agent ISO source DataVolume as Bootwright-managed", action: "ansible.builtin.command"}:                                                              {class: "mutation", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Purge the failed KubeVirt agent ISO clone PersistentVolumeClaim", action: "ansible.builtin.include_tasks"}:                                                         {class: "removal", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Purge the previous KubeVirt agent ISO PersistentVolumeClaim", action: "ansible.builtin.include_tasks"}:                                                             {class: "removal", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Purge the stale KubeVirt agent ISO source PersistentVolumeClaim", action: "ansible.builtin.include_tasks"}:                                                         {class: "removal", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Query KubeVirt VirtualMachineInstance before installer boot", action: "ansible.builtin.command"}:                                                                   {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Read KubeVirt boot VirtualMachine identity", action: "ansible.builtin.command"}:                                                                                    {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Read existing KubeVirt agent ISO DataVolume identity", action: "ansible.builtin.command"}:                                                                          {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Read existing KubeVirt agent ISO source DataVolume identity", action: "ansible.builtin.command"}:                                                                   {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Read managed KubeVirt host cluster ingress CA bundle", action: "ansible.builtin.command"}:                                                                          {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Read the KubeVirt agent ISO clone events", action: "ansible.builtin.command"}:                                                                                      {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Remove previous KubeVirt agent ISO DataVolume", action: "ansible.builtin.command"}:                                                                                 {class: "removal", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Remove stale KubeVirt agent ISO source DataVolume", action: "ansible.builtin.command"}:                                                                             {class: "removal", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Remove the failed KubeVirt agent ISO DataVolume clone", action: "ansible.builtin.command"}:                                                                         {class: "removal", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Start KubeVirt VirtualMachine", action: "ansible.builtin.command"}:                                                                                                 {class: "mutation", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Stop KubeVirt VirtualMachine before replacing the agent ISO", action: "ansible.builtin.command"}:                                                                   {class: "removal", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels", action: "ansible.builtin.command"}:                                                                 {class: "mutation", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Upload KubeVirt agent ISO DataVolume", action: "ansible.builtin.command"}:                                                                                          {class: "mutation", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Upload KubeVirt agent ISO source DataVolume", action: "ansible.builtin.command"}:                                                                                   {class: "mutation", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Wait for KubeVirt VirtualMachineInstance readiness", action: "ansible.builtin.command"}:                                                                            {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Wait for KubeVirt node readiness", action: "ansible.builtin.include_role"}:                                                                                         {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Wait for the KubeVirt VirtualMachineInstance to be created", action: "ansible.builtin.command"}:                                                                    {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Wait for the KubeVirt agent ISO DataVolume clone", action: "ansible.builtin.command"}:                                                                              {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Wait for the shared KubeVirt agent ISO source DataVolume", action: "ansible.builtin.command"}:                                                                      {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/main.yml", name: "Write managed KubeVirt host cluster ingress CA bundle", action: "ansible.builtin.copy"}:                                                                            {class: "mutation", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/purge_media_pvc.yml", name: "Remove the KubeVirt agent ISO PersistentVolumeClaim", action: "ansible.builtin.command"}:                                                                {class: "removal", surface: "kubevirt boot media lifecycle", anchor: "Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_kubevirt/tasks/purge_media_pvc.yml", name: "Wait for the KubeVirt agent ISO PersistentVolumeClaim to disappear", action: "ansible.builtin.command"}:                                                 {class: "probe", surface: "kubevirt boot media lifecycle", anchor: "Enforce KubeVirt agent ISO DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/media_insert.yml", name: "Disable HTTPS transfer certificate verification for BMC media fetch", action: "ansible.builtin.uri"}:                                                   {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/media_insert.yml", name: "Import artifact server certificate into the BMC trust store", action: "ansible.builtin.include_tasks"}:                                                 {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/media_insert.yml", name: "Refresh Redfish manager SecurityService for HTTPS file transfer", action: "ansible.builtin.uri"}:                                                       {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/media_insert.yml", name: "Retry Redfish virtual media insertion until attached", action: "ansible.builtin.include_tasks"}:                                                        {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml", name: "Refresh Redfish system metadata before disk boot override", action: "ansible.builtin.uri"}:                                                                {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml", name: "Set subsequent boots to disk after live ISO boot", action: "ansible.builtin.uri"}:                                                                         {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml", name: "Wait for node SSH readiness", action: "ansible.builtin.include_role"}:                                                                                     {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power.yml", name: "Apply Redfish boot override and power cycle", action: "ansible.builtin.import_tasks"}:                                                                         {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power.yml", name: "Insert Redfish virtual media", action: "ansible.builtin.import_tasks"}:                                                                                        {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml", name: "Force power off (tolerate already-off)", action: "ansible.builtin.uri"}:                                                                              {class: "removal", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml", name: "Power on", action: "ansible.builtin.uri"}:                                                                                                            {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml", name: "Probe Redfish Reset ActionInfo", action: "ansible.builtin.uri"}:                                                                                      {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml", name: "Refresh Redfish system metadata before reset and boot override", action: "ansible.builtin.uri"}:                                                      {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml", name: "Set one-time boot to CD", action: "ansible.builtin.uri"}:                                                                                             {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml", name: "Wait for BMC to report PowerState=Off", action: "ansible.builtin.include_tasks"}:                                                                     {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml", name: "Wait for BMC to report PowerState=On", action: "ansible.builtin.include_tasks"}:                                                                      {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_probe.yml", name: "Probe Redfish system power state", action: "ansible.builtin.uri"}:                                                                                 {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_probe.yml", name: "Wait before retrying Redfish power state probe", action: "ansible.builtin.command"}:                                                               {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_wait.yml", name: "Retry Redfish power state probe until reached", action: "ansible.builtin.include_tasks"}:                                                           {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_wait.yml", name: "Run Redfish power state probe", action: "ansible.builtin.include_tasks"}:                                                                           {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Load Redfish credentials", action: "ansible.builtin.import_tasks"}:                                                                                                  {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Power node from virtual media", action: "ansible.builtin.import_tasks"}:                                                                                             {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Prepare Redfish virtual media", action: "ansible.builtin.import_tasks"}:                                                                                             {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Prove required TPM 2.0 before Redfish mutation", action: "ansible.builtin.import_tasks"}:                                                                            {class: "gate", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Require a powered-off machine before installer boot", action: "ansible.builtin.import_tasks"}:                                                                       {class: "gate", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Resolve Redfish system", action: "ansible.builtin.import_tasks"}:                                                                                                    {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Set post-boot Redfish boot device", action: "ansible.builtin.import_tasks"}:                                                                                         {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Validate Redfish boot inputs", action: "ansible.builtin.import_tasks"}:                                                                                              {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml", name: "Validate declared MACs against Redfish inventory", action: "ansible.builtin.import_tasks"}:                                                                          {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/eject.yml", name: "Eject virtual media", action: "ansible.builtin.uri"}:                                                                                                         {class: "removal", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/eject.yml", name: "Wait for virtual media to report ejected", action: "ansible.builtin.uri"}:                                                                                    {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate.yml", name: "Import the artifact certificate via the discovered trust-store method", action: "ansible.builtin.include_tasks"}:                                {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate.yml", name: "Probe Redfish SecurityService for a manager OEM root-CA import action", action: "ansible.builtin.uri"}:                                          {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate.yml", name: "Refresh VirtualMedia member to locate a DMTF certificate store", action: "ansible.builtin.uri"}:                                                 {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate.yml", name: "Retrieve the artifact server certificate", action: "ansible.builtin.command"}:                                                                   {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate/security_service.yml", name: "Import the artifact certificate as a remote HTTPS server root CA (SecurityService)", action: "ansible.builtin.uri"}:            {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate/standard.yml", name: "Import the artifact certificate via the DMTF VirtualMedia store", action: "ansible.builtin.uri"}:                                       {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Disable HTTPS certificate verification for virtual-media fetch", action: "ansible.builtin.uri"}:                                                     {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Eject virtual media before retrying insertion", action: "ansible.builtin.uri"}:                                                                      {class: "removal", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Insert the agent ISO as virtual media", action: "ansible.builtin.uri"}:                                                                              {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Patch virtual media attachment when InsertMedia action is not reflected", action: "ansible.builtin.uri"}:                                            {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Probe virtual media after failed InsertMedia task", action: "ansible.builtin.uri"}:                                                                  {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Probe virtual media after mounted InsertMedia task", action: "ansible.builtin.uri"}:                                                                 {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Refresh virtual media metadata before PATCH operations", action: "ansible.builtin.uri"}:                                                             {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Wait before retrying Redfish virtual media insertion", action: "ansible.builtin.command"}:                                                           {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Wait for Redfish InsertMedia task to complete", action: "ansible.builtin.uri"}:                                                                      {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Wait for virtual media to report inserted agent ISO after PATCH fallback", action: "ansible.builtin.uri"}:                                           {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml", name: "Wait for virtual media to report inserted agent ISO", action: "ansible.builtin.uri"}:                                                                {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_until_attached.yml", name: "Retry Redfish virtual media insertion when not attached", action: "ansible.builtin.include_tasks"}:                                           {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_until_attached.yml", name: "Run Redfish virtual media insertion attempt", action: "ansible.builtin.include_tasks"}:                                                       {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml", name: "Eject any previously inserted virtual media (idempotent)", action: "ansible.builtin.include_tasks"}:                                                        {class: "removal", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml", name: "List Redfish managers", action: "ansible.builtin.uri"}:                                                                                                     {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml", name: "List VirtualMedia members for Redfish managers", action: "ansible.builtin.uri"}:                                                                            {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml", name: "List VirtualMedia members for the target system", action: "ansible.builtin.uri"}:                                                                           {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml", name: "Probe Redfish VMM ActionInfo", action: "ansible.builtin.uri"}:                                                                                              {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml", name: "Probe Redfish VirtualMedia members", action: "ansible.builtin.uri"}:                                                                                        {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml", name: "Run virtual-media backend preparation", action: "ansible.builtin.include_role"}:                                                                            {class: "delegated", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/remove_certificate.yml", name: "Remove the imported artifact certificate via the discovered trust-store method", action: "ansible.builtin.include_tasks"}:                       {class: "removal", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/remove_certificate/security_service.yml", name: "Remove imported artifact root CA from the SecurityService store", action: "ansible.builtin.uri"}:                               {class: "removal", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/remove_certificate/standard.yml", name: "Remove imported artifact server certificate from the DMTF VirtualMedia store", action: "ansible.builtin.uri"}:                          {class: "removal", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/restore_certificate_verification.yml", name: "Refresh Redfish manager SecurityService before restoring HTTPS transfer certificate verification", action: "ansible.builtin.uri"}: {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/restore_certificate_verification.yml", name: "Refresh virtual media metadata before restoring fetch certificate verification", action: "ansible.builtin.uri"}:                   {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/restore_certificate_verification.yml", name: "Remove imported artifact server certificate from the BMC trust store", action: "ansible.builtin.import_tasks"}:                    {class: "removal", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/restore_certificate_verification.yml", name: "Restore Redfish HTTPS transfer certificate verification", action: "ansible.builtin.uri"}:                                          {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/restore_certificate_verification.yml", name: "Restore virtual media fetch certificate verification", action: "ansible.builtin.uri"}:                                             {class: "mutation", surface: "redfish boot lifecycle", anchor: "Prepare Redfish virtual media"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/stage/credentials.yml", name: "Load Redfish credentials", action: "ansible.builtin.include_role"}:                                                                                     {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/stage/system.yml", name: "Discover Redfish System ID", action: "ansible.builtin.uri"}:                                                                                                 {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/macs.yml", name: "List Redfish EthernetInterface members", action: "ansible.builtin.uri"}:                                                                                  {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/macs.yml", name: "Probe Redfish EthernetInterface members", action: "ansible.builtin.uri"}:                                                                                 {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/macs.yml", name: "Refresh Redfish system metadata for declared MAC validation", action: "ansible.builtin.uri"}:                                                             {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/power.yml", name: "Query Redfish system power state before installer boot", action: "ansible.builtin.uri"}:                                                                 {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/tpm2.yml", name: "Probe Redfish ComputerSystem TPM 2.0 inventory", action: "ansible.builtin.uri"}:                                                                          {class: "probe", surface: "redfish boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/cleanup_media.yml", name: "Clean vSphere virtual media", action: "ansible.builtin.include_role"}:                                                                                      {class: "removal", surface: "vsphere media cleanup", anchor: "Remove vSphere virtual media drive"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml", name: "Clean vSphere virtual media", action: "ansible.builtin.import_tasks"}:                                                                                               {class: "removal", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml", name: "Power on vSphere machine", action: "ansible.builtin.import_tasks"}:                                                                                                  {class: "delegated", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml", name: "Prepare vSphere virtual media", action: "ansible.builtin.import_tasks"}:                                                                                             {class: "delegated", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml", name: "Require a powered-off machine before installer boot", action: "ansible.builtin.import_tasks"}:                                                                       {class: "gate", surface: "vsphere boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml", name: "Resolve vSphere boot context", action: "ansible.builtin.import_tasks"}:                                                                                              {class: "probe", surface: "vsphere boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml", name: "Stage generated agent ISO", action: "ansible.builtin.import_tasks"}:                                                                                                 {class: "delegated", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml", name: "Wait for vSphere node readiness", action: "ansible.builtin.include_role"}:                                                                                           {class: "probe", surface: "vsphere boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/power.yml", name: "Power off vSphere machine", action: "community.vmware.vmware_guest_powerstate"}:                                                                                    {class: "removal", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/power.yml", name: "Power on vSphere machine", action: "community.vmware.vmware_guest_powerstate"}:                                                                                     {class: "mutation", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/power_gate.yml", name: "Load vCenter credentials for the power gate", action: "ansible.builtin.include_role"}:                                                                         {class: "gate", surface: "vsphere boot lifecycle", anchor: "Require a powered-off machine before installer boot"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/prepare_media.yml", name: "Load vCenter credentials", action: "ansible.builtin.include_role"}:                                                                                         {class: "probe", surface: "vsphere media replacement", anchor: "Require exact vSphere virtual media ownership before replacement"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/prepare_media.yml", name: "Prepare vSphere virtual media", action: "ansible.builtin.include_role"}:                                                                                    {class: "delegated", surface: "vsphere media replacement", anchor: "Delete superseded vSphere virtual media from the datastore"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/stage_agent_iso.yml", name: "Create vSphere media staging directory", action: "ansible.builtin.file"}:                                                                                 {class: "mutation", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/stage_agent_iso.yml", name: "Mark generated agent ISO staged", action: "ansible.builtin.copy"}:                                                                                        {class: "mutation", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/stage_agent_iso.yml", name: "Stage generated agent ISO", action: "ansible.builtin.copy"}:                                                                                              {class: "mutation", surface: "vsphere boot lifecycle", anchor: "Stage generated agent ISO"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/eject.yml", name: "Clean libvirt virtual media from {{ bootwright_libvirt_media_scope }} domain", action: "ansible.builtin.command"}:                                                 {class: "removal", surface: "libvirt media lifecycle", anchor: "Clean stale running virtual media before insert"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/eject.yml", name: "Install virtual media eject helper", action: "ansible.builtin.copy"}:                                                                                              {class: "removal", surface: "libvirt media lifecycle", anchor: "Clean stale running virtual media before insert"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/main.yml", name: "Clean stale persistent virtual media before insert", action: "ansible.builtin.include_tasks"}:                                                                      {class: "removal", surface: "libvirt media lifecycle", anchor: "Clean stale running virtual media before insert"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/main.yml", name: "Clean stale running virtual media before insert", action: "ansible.builtin.include_tasks"}:                                                                         {class: "removal", surface: "libvirt media lifecycle", anchor: "Clean stale running virtual media before insert"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/main.yml", name: "Insert staged virtual media directly into libvirt domain", action: "ansible.builtin.command"}:                                                                      {class: "mutation", surface: "libvirt media lifecycle", anchor: "Clean stale running virtual media before insert"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/main.yml", name: "Install virtual media insert helper", action: "ansible.builtin.copy"}:                                                                                              {class: "mutation", surface: "libvirt media lifecycle", anchor: "Clean stale running virtual media before insert"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/cleanup.yml", name: "Delete uploaded vSphere virtual media from the datastore", action: "community.vmware.vsphere_file"}:                                                             {class: "removal", surface: "vsphere media cleanup", anchor: "Remove vSphere virtual media drive"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/cleanup.yml", name: "Remove recorded vSphere virtual media staging paths and record", action: "ansible.builtin.include_role"}:                                                        {class: "removal", surface: "vsphere media cleanup", anchor: "Remove vSphere virtual media drive"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/cleanup.yml", name: "Remove vSphere virtual media drive", action: "community.vmware.vmware_guest"}:                                                                                   {class: "removal", surface: "vsphere media cleanup", anchor: "Remove vSphere virtual media drive"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/context.yml", name: "Load vCenter credentials", action: "ansible.builtin.include_role"}:                                                                                              {class: "probe", surface: "vsphere media replacement", anchor: "Require exact vSphere virtual media ownership before replacement"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/insert.yml", name: "Attach vSphere virtual media to the machine CD-ROM", action: "community.vmware.vmware_guest"}:                                                                    {class: "mutation", surface: "vsphere media replacement", anchor: "Delete superseded vSphere virtual media from the datastore"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/insert.yml", name: "Delete superseded vSphere virtual media from the datastore", action: "community.vmware.vsphere_file"}:                                                            {class: "removal", surface: "vsphere media replacement", anchor: "Delete superseded vSphere virtual media from the datastore"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/insert.yml", name: "Mark vSphere virtual media uploaded", action: "ansible.builtin.copy"}:                                                                                            {class: "mutation", surface: "vsphere media replacement", anchor: "Delete superseded vSphere virtual media from the datastore"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/insert.yml", name: "Record vSphere virtual media ownership", action: "ansible.builtin.include_role"}:                                                                                 {class: "evidence", surface: "vsphere media replacement", anchor: "Remove superseded vSphere virtual media staging paths and record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/insert.yml", name: "Remove superseded vSphere virtual media staging paths and record", action: "ansible.builtin.include_role"}:                                                       {class: "removal", surface: "vsphere media replacement", anchor: "Delete superseded vSphere virtual media from the datastore"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/insert.yml", name: "Upload vSphere virtual media to the datastore", action: "community.vmware.vsphere_copy"}:                                                                         {class: "mutation", surface: "vsphere media replacement", anchor: "Delete superseded vSphere virtual media from the datastore"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/main.yml", name: "Clean up vSphere virtual media", action: "ansible.builtin.import_tasks"}:                                                                                           {class: "removal", surface: "vsphere media replacement", anchor: "Delete superseded vSphere virtual media from the datastore"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/main.yml", name: "Insert vSphere virtual media", action: "ansible.builtin.import_tasks"}:                                                                                             {class: "delegated", surface: "vsphere media replacement", anchor: "Delete superseded vSphere virtual media from the datastore"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/main.yml", name: "Resolve vSphere virtual media context", action: "ansible.builtin.import_tasks"}:                                                                                    {class: "probe", surface: "vsphere media replacement", anchor: "Require exact vSphere virtual media ownership before replacement"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_baremetal/tasks/destroy.yml", name: "Remove cluster baremetal state directory (idempotent)", action: "ansible.builtin.file"}:                                                                             {class: "removal", surface: "baremetal substrate destroy", anchor: "Remove managed OS install artifacts"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_baremetal/tasks/destroy.yml", name: "Remove managed OS install artifacts", action: "ansible.builtin.include_role"}:                                                                                       {class: "removal", surface: "baremetal substrate destroy", anchor: "Remove managed OS install artifacts"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_baremetal/tasks/destroy.yml", name: "Remove per-cluster OS-install state after the last machine", action: "ansible.builtin.file"}:                                                                        {class: "removal", surface: "baremetal substrate destroy", anchor: "Remove managed OS install artifacts"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_baremetal/tasks/main.yml", name: "Create baremetal state directory", action: "ansible.builtin.file"}:                                                                                                     {class: "mutation", surface: "baremetal substrate apply", anchor: "Create baremetal state directory"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_baremetal/tasks/main.yml", name: "Record baremetal machine manifest", action: "ansible.builtin.template"}:                                                                                                {class: "evidence", surface: "baremetal substrate apply", anchor: "Record baremetal machine manifest"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Delete KubeVirt DataVolumes", action: "ansible.builtin.command"}:                                                                                                     {class: "removal", surface: "kubevirt substrate destroy", anchor: "Delete KubeVirt VirtualMachine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Delete KubeVirt VirtualMachine", action: "ansible.builtin.command"}:                                                                                                  {class: "removal", surface: "kubevirt substrate destroy", anchor: "Delete KubeVirt VirtualMachine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Delete the shared KubeVirt agent ISO source DataVolume", action: "ansible.builtin.command"}:                                                                          {class: "removal", surface: "kubevirt substrate destroy", anchor: "Delete KubeVirt VirtualMachine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Probe KubeVirt host cluster reachability", action: "ansible.builtin.command"}:                                                                                        {class: "probe", surface: "kubevirt substrate destroy", anchor: "Refuse to delete a foreign KubeVirt PersistentVolumeClaim"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Purge the KubeVirt agent ISO PersistentVolumeClaim", action: "ansible.builtin.include_role"}:                                                                         {class: "removal", surface: "kubevirt substrate destroy", anchor: "Delete KubeVirt VirtualMachine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Purge the KubeVirt root disk PersistentVolumeClaim", action: "ansible.builtin.include_role"}:                                                                         {class: "removal", surface: "kubevirt substrate destroy", anchor: "Delete KubeVirt VirtualMachine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Purge the shared KubeVirt agent ISO source PersistentVolumeClaim", action: "ansible.builtin.include_role"}:                                                           {class: "removal", surface: "kubevirt substrate destroy", anchor: "Delete KubeVirt VirtualMachine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Read KubeVirt DataVolume identities", action: "ansible.builtin.command"}:                                                                                             {class: "probe", surface: "kubevirt substrate destroy", anchor: "Refuse to delete a foreign KubeVirt PersistentVolumeClaim"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Read KubeVirt PersistentVolumeClaim identities", action: "ansible.builtin.command"}:                                                                                  {class: "probe", surface: "kubevirt substrate destroy", anchor: "Refuse to delete a foreign KubeVirt PersistentVolumeClaim"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Read KubeVirt VirtualMachine identity", action: "ansible.builtin.command"}:                                                                                           {class: "probe", surface: "kubevirt substrate destroy", anchor: "Refuse to delete a foreign KubeVirt PersistentVolumeClaim"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Remove KubeVirt machine ownership record", action: "ansible.builtin.include_role"}:                                                                                   {class: "evidence", surface: "kubevirt substrate destroy", anchor: "Remove KubeVirt machine ownership record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/destroy.yml", name: "Stamp inherited KubeVirt PersistentVolumeClaim identities", action: "ansible.builtin.command"}:                                                                       {class: "mutation", surface: "kubevirt substrate destroy", anchor: "Delete KubeVirt VirtualMachine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Apply KubeVirt VirtualMachine", action: "ansible.builtin.command"}:                                                                                                      {class: "mutation", surface: "kubevirt substrate apply", anchor: "Apply KubeVirt root disk DataVolume"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Apply KubeVirt root disk DataVolume", action: "ansible.builtin.command"}:                                                                                                {class: "mutation", surface: "kubevirt substrate apply", anchor: "Apply KubeVirt root disk DataVolume"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Collect KubeVirt root disk provisioning state when the DataVolume did not become ready", action: "ansible.builtin.command"}:                                             {class: "mutation", surface: "kubevirt substrate apply", anchor: "Apply KubeVirt root disk DataVolume"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Delete KubeVirt VirtualMachine for authorized rebuild", action: "ansible.builtin.command"}:                                                                              {class: "removal", surface: "kubevirt substrate apply", anchor: "Apply KubeVirt root disk DataVolume"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Delete KubeVirt root DataVolume for authorized rebuild", action: "ansible.builtin.command"}:                                                                             {class: "removal", surface: "kubevirt substrate apply", anchor: "Apply KubeVirt root disk DataVolume"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Enforce KubeVirt VirtualMachine apply mode", action: "ansible.builtin.include_role"}:                                                                                    {class: "gate", surface: "kubevirt substrate apply", anchor: "Enforce KubeVirt root DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Enforce KubeVirt root DataVolume apply mode", action: "ansible.builtin.include_role"}:                                                                                   {class: "gate", surface: "kubevirt substrate apply", anchor: "Enforce KubeVirt root DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Ensure KubeVirt namespace exists", action: "ansible.builtin.command"}:                                                                                                   {class: "mutation", surface: "kubevirt substrate apply", anchor: "Apply KubeVirt root disk DataVolume"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Probe configured KubeVirt storage class", action: "ansible.builtin.command"}:                                                                                            {class: "probe", surface: "kubevirt substrate apply", anchor: "Enforce KubeVirt root DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Purge the KubeVirt root disk PersistentVolumeClaim for authorized rebuild", action: "ansible.builtin.include_role"}:                                                     {class: "removal", surface: "kubevirt substrate apply", anchor: "Apply KubeVirt root disk DataVolume"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Read KubeVirt storage profile volume mode", action: "ansible.builtin.command"}:                                                                                          {class: "probe", surface: "kubevirt substrate apply", anchor: "Enforce KubeVirt root DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Read existing KubeVirt VirtualMachine identity", action: "ansible.builtin.command"}:                                                                                     {class: "probe", surface: "kubevirt substrate apply", anchor: "Enforce KubeVirt root DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Read existing KubeVirt root DataVolume identity", action: "ansible.builtin.command"}:                                                                                    {class: "probe", surface: "kubevirt substrate apply", anchor: "Enforce KubeVirt root DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Read reused KubeVirt root claim volume mode", action: "ansible.builtin.command"}:                                                                                        {class: "probe", surface: "kubevirt substrate apply", anchor: "Enforce KubeVirt root DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Record KubeVirt machine ownership", action: "ansible.builtin.include_role"}:                                                                                             {class: "evidence", surface: "kubevirt substrate apply", anchor: "Record KubeVirt machine ownership"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Stop KubeVirt VirtualMachine for authorized rebuild", action: "ansible.builtin.command"}:                                                                                {class: "removal", surface: "kubevirt substrate apply", anchor: "Apply KubeVirt root disk DataVolume"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml", name: "Wait for KubeVirt root disk DataVolume", action: "ansible.builtin.command"}:                                                                                             {class: "probe", surface: "kubevirt substrate apply", anchor: "Enforce KubeVirt root DataVolume apply mode"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Read libvirt domain ownership metadata", action: "ansible.builtin.command"}:                                                                                           {class: "probe", surface: "libvirt domain destroy", anchor: "Refuse to destroy a non-Bootwright libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Remove libvirt domain ownership record", action: "ansible.builtin.include_role"}:                                                                                      {class: "evidence", surface: "libvirt domain destroy", anchor: "Remove libvirt domain ownership record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Remove machine state directory", action: "ansible.builtin.file"}:                                                                                                      {class: "removal", surface: "libvirt domain destroy", anchor: "Stop libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Remove machine storage directory", action: "ansible.builtin.file"}:                                                                                                    {class: "removal", surface: "libvirt domain destroy", anchor: "Stop libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Remove managed OS install artifacts", action: "ansible.builtin.include_role"}:                                                                                         {class: "removal", surface: "libvirt domain destroy", anchor: "Stop libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Remove per-cluster OS-install state after the last machine", action: "ansible.builtin.file"}:                                                                          {class: "removal", surface: "libvirt domain destroy", anchor: "Stop libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Stop libvirt domain", action: "ansible.builtin.command"}:                                                                                                              {class: "removal", surface: "libvirt domain destroy", anchor: "Stop libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Undefine libvirt domain", action: "ansible.builtin.command"}:                                                                                                          {class: "removal", surface: "libvirt domain destroy", anchor: "Stop libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/destroy.yml", name: "Verify the libvirt domain is absent before disk deletion", action: "ansible.builtin.command"}:                                                                         {class: "probe", surface: "libvirt domain destroy", anchor: "Refuse to destroy a non-Bootwright libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Align libvirt disk image ownership", action: "ansible.builtin.file"}:                                                                                                  {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Create machine data disks ({{ bootwright_libvirt_data_disks | length }} configured)", action: "ansible.builtin.command"}:                                              {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Create machine disk", action: "ansible.builtin.command"}:                                                                                                              {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Create per-machine libvirt state directories", action: "ansible.builtin.file"}:                                                                                        {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Define libvirt domain", action: "community.libvirt.virt"}:                                                                                                             {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Enforce libvirt domain apply mode against live state", action: "ansible.builtin.include_role"}:                                                                        {class: "gate", surface: "libvirt domain apply", anchor: "Enforce libvirt domain apply mode against live state"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Migrate machine data disks to libvirt storage ({{ bootwright_libvirt_data_disks | length }} configured)", action: "ansible.builtin.command"}:                          {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Migrate machine disk to libvirt storage", action: "ansible.builtin.command"}:                                                                                          {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Probe existing libvirt root disk size", action: "ansible.builtin.command"}:                                                                                            {class: "probe", surface: "libvirt domain apply", anchor: "Enforce libvirt domain apply mode against live state"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Probe libvirt runtime users", action: "ansible.builtin.command"}:                                                                                                      {class: "probe", surface: "libvirt domain apply", anchor: "Enforce libvirt domain apply mode against live state"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Read libvirt domain ownership metadata for apply", action: "ansible.builtin.command"}:                                                                                 {class: "probe", surface: "libvirt domain apply", anchor: "Enforce libvirt domain apply mode against live state"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Record libvirt domain ownership", action: "ansible.builtin.include_role"}:                                                                                             {class: "evidence", surface: "libvirt domain apply", anchor: "Record libvirt domain ownership"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Recreate managed OS libvirt machine directories after override reset", action: "ansible.builtin.file"}:                                                                {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Remove managed OS libvirt machine state for override reinstall", action: "ansible.builtin.file"}:                                                                      {class: "removal", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Render libvirt domain XML", action: "ansible.builtin.template"}:                                                                                                       {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Restore libvirt storage labels", action: "ansible.builtin.command"}:                                                                                                   {class: "mutation", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Stop managed OS libvirt domain for override reinstall", action: "ansible.builtin.command"}:                                                                            {class: "removal", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Undefine managed OS libvirt domain for override reinstall", action: "ansible.builtin.command"}:                                                                        {class: "removal", surface: "libvirt domain apply", anchor: "Define libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml", name: "Verify the reset libvirt domain is absent before disk deletion", action: "ansible.builtin.command"}:                                                                   {class: "probe", surface: "libvirt domain apply", anchor: "Enforce libvirt domain apply mode against live state"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml", name: "Activate libvirt network", action: "community.libvirt.virt_net"}:                                                                                                      {class: "mutation", surface: "libvirt network apply", anchor: "Define libvirt network"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml", name: "Create per-cluster libvirt state directory", action: "ansible.builtin.file"}:                                                                                          {class: "mutation", surface: "libvirt network apply", anchor: "Define libvirt network"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml", name: "Define libvirt network", action: "community.libvirt.virt_net"}:                                                                                                        {class: "mutation", surface: "libvirt network apply", anchor: "Define libvirt network"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml", name: "Ensure libvirt host packages and service", action: "ansible.builtin.import_role"}:                                                                                     {class: "delegated", surface: "libvirt network apply", anchor: "Define libvirt network"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml", name: "Read live libvirt network definition before apply", action: "ansible.builtin.command"}:                                                                                {class: "probe", surface: "libvirt network apply", anchor: "Refuse to redefine a foreign libvirt network"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml", name: "Record libvirt network ownership", action: "ansible.builtin.include_role"}:                                                                                            {class: "evidence", surface: "libvirt network apply", anchor: "Record libvirt network ownership"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml", name: "Render libvirt network XML", action: "ansible.builtin.template"}:                                                                                                      {class: "mutation", surface: "libvirt network apply", anchor: "Define libvirt network"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/apply.yml", name: "Apply vSphere virtual machine", action: "community.vmware.vmware_guest"}:                                                                                                {class: "mutation", surface: "vsphere apply dispatch", anchor: "Apply vSphere virtual machine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/apply.yml", name: "Enforce vSphere boot order", action: "community.vmware.vmware_guest_boot_manager"}:                                                                                      {class: "mutation", surface: "vsphere apply dispatch", anchor: "Apply vSphere virtual machine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/apply.yml", name: "Power on the cloned vSphere machine", action: "community.vmware.vmware_guest_powerstate"}:                                                                               {class: "mutation", surface: "vsphere apply dispatch", anchor: "Apply vSphere virtual machine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/context.yml", name: "Load vCenter credentials", action: "ansible.builtin.include_role"}:                                                                                                    {class: "probe", surface: "vsphere apply dispatch", anchor: "Gate vSphere virtual machine changes"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy.yml", name: "Delete recorded vSphere virtual media from the datastore", action: "ansible.builtin.include_tasks"}:                                                                   {class: "removal", surface: "vsphere substrate destroy", anchor: "Delete vSphere VM"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy.yml", name: "Delete vSphere VM", action: "community.vmware.vmware_guest"}:                                                                                                          {class: "removal", surface: "vsphere substrate destroy", anchor: "Delete vSphere VM"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy.yml", name: "Load vCenter credentials", action: "ansible.builtin.include_role"}:                                                                                                    {class: "probe", surface: "vsphere substrate destroy", anchor: "Refuse to delete a non-Bootwright vSphere VM"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy.yml", name: "Remove managed OS install artifacts", action: "ansible.builtin.include_role"}:                                                                                         {class: "removal", surface: "vsphere substrate destroy", anchor: "Delete vSphere VM"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy.yml", name: "Remove per-cluster OS-install state after the last machine", action: "ansible.builtin.file"}:                                                                          {class: "removal", surface: "vsphere substrate destroy", anchor: "Delete vSphere VM"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy.yml", name: "Remove recorded vSphere virtual media staging paths and records", action: "ansible.builtin.include_role"}:                                                             {class: "removal", surface: "vsphere substrate destroy", anchor: "Delete vSphere VM"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy.yml", name: "Remove vSphere machine ownership record", action: "ansible.builtin.include_role"}:                                                                                     {class: "evidence", surface: "vsphere substrate destroy", anchor: "Remove vSphere machine ownership record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/destroy_vmedia.yml", name: "Delete recorded vSphere virtual media file", action: "community.vmware.vsphere_file"}:                                                                          {class: "removal", surface: "vsphere substrate destroy", anchor: "Delete vSphere VM"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/main.yml", name: "Apply vSphere virtual machine", action: "ansible.builtin.import_tasks"}:                                                                                                  {class: "delegated", surface: "vsphere apply dispatch", anchor: "Apply vSphere virtual machine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/main.yml", name: "Gate vSphere virtual machine changes", action: "ansible.builtin.import_tasks"}:                                                                                           {class: "gate", surface: "vsphere apply dispatch", anchor: "Gate vSphere virtual machine changes"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/main.yml", name: "Probe vSphere virtual machine", action: "ansible.builtin.import_tasks"}:                                                                                                  {class: "probe", surface: "vsphere apply dispatch", anchor: "Gate vSphere virtual machine changes"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/main.yml", name: "Record vSphere machine ownership", action: "ansible.builtin.import_tasks"}:                                                                                               {class: "evidence", surface: "vsphere apply dispatch", anchor: "Record vSphere machine ownership"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/main.yml", name: "Resolve vSphere VM layout", action: "ansible.builtin.import_tasks"}:                                                                                                      {class: "probe", surface: "vsphere apply dispatch", anchor: "Gate vSphere virtual machine changes"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/main.yml", name: "Resolve vSphere substrate context", action: "ansible.builtin.import_tasks"}:                                                                                              {class: "probe", surface: "vsphere apply dispatch", anchor: "Gate vSphere virtual machine changes"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/ownership.yml", name: "Record vSphere machine ownership", action: "ansible.builtin.include_role"}:                                                                                          {class: "evidence", surface: "vsphere apply dispatch", anchor: "Record vSphere machine ownership"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/probe.yml", name: "Enforce vSphere VM apply mode against live state", action: "ansible.builtin.include_role"}:                                                                              {class: "gate", surface: "vsphere apply dispatch", anchor: "Gate vSphere virtual machine changes"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/probe.yml", name: "Remove managed OS vSphere VM for override reinstall", action: "community.vmware.vmware_guest"}:                                                                          {class: "removal", surface: "vsphere apply dispatch", anchor: "Apply vSphere virtual machine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/probe.yml", name: "Stop managed OS vSphere VM for override reinstall", action: "community.vmware.vmware_guest_powerstate"}:                                                                 {class: "removal", surface: "vsphere apply dispatch", anchor: "Apply vSphere virtual machine"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_declared_managed_os.yml", name: "Destroy exactly recorded declared managed OS artifacts", action: "ansible.builtin.include_tasks"}:                                                          {class: "removal", surface: "recorded managed OS destroy", anchor: "Destroy exactly recorded declared managed OS artifacts"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_record_gate.yml", name: "Gate recorded BMC emulator by live identity", action: "ansible.builtin.include_tasks"}:                                                                             {class: "gate", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_record_gate.yml", name: "Gate recorded infra component by live identity", action: "ansible.builtin.include_tasks"}:                                                                          {class: "gate", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_record_gate.yml", name: "Gate recorded libvirt resource by live identity", action: "ansible.builtin.include_tasks"}:                                                                         {class: "gate", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_record_gate.yml", name: "Gate recorded managed OS artifacts by live identity", action: "ansible.builtin.include_tasks"}:                                                                     {class: "gate", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_record_libvirt_gate.yml", name: "Probe recorded live libvirt resource XML", action: "ansible.builtin.command"}:                                                                              {class: "probe", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Close recorded firewalld ports", action: "ansible.posix.firewalld"}:                                                                                                   {class: "mutation", surface: "recorded provider resource destroy", anchor: "Stop recorded libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "List active mounts before removing owned paths", action: "ansible.builtin.command"}:                                                                                   {class: "probe", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Remove destroyed ownership resource record", action: "ansible.builtin.include_role"}:                                                                                  {class: "evidence", surface: "recorded provider resource destroy", anchor: "Remove destroyed ownership resource record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Remove recorded owned paths", action: "ansible.builtin.file"}:                                                                                                         {class: "removal", surface: "recorded provider resource destroy", anchor: "Stop recorded libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Remove recorded podman container", action: "containers.podman.podman_container"}:                                                                                      {class: "removal", surface: "recorded provider resource destroy", anchor: "Stop recorded libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Revalidate recorded resource before teardown", action: "ansible.builtin.include_tasks"}:                                                                               {class: "gate", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Stop recorded libvirt domain", action: "ansible.builtin.command"}:                                                                                                     {class: "removal", surface: "recorded provider resource destroy", anchor: "Stop recorded libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Stop recorded libvirt network", action: "ansible.builtin.command"}:                                                                                                    {class: "removal", surface: "recorded provider resource destroy", anchor: "Stop recorded libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Undefine recorded libvirt domain", action: "ansible.builtin.command"}:                                                                                                 {class: "removal", surface: "recorded provider resource destroy", anchor: "Stop recorded libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Undefine recorded libvirt network", action: "ansible.builtin.command"}:                                                                                                {class: "removal", surface: "recorded provider resource destroy", anchor: "Stop recorded libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Unmount active mounts under recorded owned paths", action: "ansible.posix.mount"}:                                                                                     {class: "removal", surface: "recorded provider resource destroy", anchor: "Stop recorded libvirt domain"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Validate live ownership evidence before recorded resource mutation", action: "ansible.builtin.include_tasks"}:                                                         {class: "probe", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Enforce apply mode for {{ bootwright_component.name }}", action: "ansible.builtin.include_tasks"}:                                                       {class: "gate", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Probe Bootwright-owned container for {{ bootwright_component.name }}", action: "ansible.builtin.command"}:                                               {class: "probe", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Probe any same-named container for {{ bootwright_component.name }}", action: "ansible.builtin.command"}:                                                 {class: "probe", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Read owning context from existing {{ bootwright_component.name }} container", action: "ansible.builtin.command"}:                                        {class: "probe", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_destroy_gate.yml", name: "Probe same-named infra component container", action: "ansible.builtin.command"}:                                                                           {class: "probe", surface: "recorded provider resource destroy", anchor: "Require live record or positive all-members-absent replay"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_shared_service_operation.yml", name: "Atomically finalize exact host-wide shared-service operation", action: "bootwright.core.claim_cas"}:                                              {class: "removal", surface: "host shared-service operation finalization", anchor: "Atomically finalize exact host-wide shared-service operation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_apply.yml", name: "Create package ownership directory", action: "ansible.builtin.file"}:                                                                                                     {class: "mutation", surface: "package ownership apply", anchor: "Install owned packages"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_apply.yml", name: "Install owned packages", action: "ansible.builtin.package"}:                                                                                                              {class: "mutation", surface: "package ownership apply", anchor: "Install owned packages"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_apply.yml", name: "Write ownership records for owned packages", action: "ansible.builtin.include_tasks"}:                                                                                    {class: "evidence", surface: "package ownership apply", anchor: "Write ownership records for owned packages"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_apply_one.yml", name: "Write package ownership record", action: "ansible.builtin.include_tasks"}:                                                                                            {class: "evidence", surface: "package ownership apply", anchor: "Write ownership records for owned packages"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_records_write.yml", name: "Write package ownership records", action: "ansible.builtin.copy"}:                                                                                                {class: "evidence", surface: "package ownership apply", anchor: "Write ownership records for owned packages"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove.yml", name: "Remove ownership-gated packages", action: "ansible.builtin.include_tasks"}:                                                                                              {class: "removal", surface: "package ownership removal", anchor: "Remove ownership-gated packages"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove_one.yml", name: "Coerce existing package requiredBy to a list", action: "ansible.builtin.include_tasks"}:                                                                             {class: "probe", surface: "package ownership member removal", anchor: "Stat package ownership record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove_one.yml", name: "Remove package ownership record", action: "ansible.builtin.file"}:                                                                                                   {class: "evidence", surface: "package ownership member removal", anchor: "Remove package ownership record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove_one.yml", name: "Remove package that Bootwright introduced", action: "ansible.builtin.package"}:                                                                                      {class: "removal", surface: "package ownership member removal", anchor: "Remove package that Bootwright introduced"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove_one.yml", name: "Update package ownership record for remaining requirements", action: "ansible.builtin.copy"}:                                                                        {class: "mutation", surface: "package ownership member removal", anchor: "Remove package that Bootwright introduced"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/remove_resource.yml", name: "Remove ownership resource record", action: "ansible.builtin.file"}:                                                                                                     {class: "removal", surface: "ownership record removal", anchor: "Remove ownership resource record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/resource.yml", name: "Create ownership resource directory", action: "ansible.builtin.file"}:                                                                                                         {class: "mutation", surface: "ownership record write", anchor: "Create ownership resource directory"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/resource.yml", name: "Write ownership resource record", action: "ansible.builtin.copy"}:                                                                                                             {class: "evidence", surface: "ownership record write", anchor: "Write ownership resource record"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy.yml", name: "Remove libvirt packages introduced by Bootwright", action: "ansible.builtin.include_role"}:                                                                                {class: "removal", surface: "libvirt provider host destroy", anchor: "Remove libvirt packages introduced by Bootwright"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Inspect libvirt domain block devices", action: "ansible.builtin.command"}:                                                                                         {class: "probe", surface: "libvirt context sweep", anchor: "Require conclusive libvirt ownership probes before context sweep"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Inspect libvirt domain ownership metadata", action: "ansible.builtin.command"}:                                                                                    {class: "probe", surface: "libvirt context sweep", anchor: "Require conclusive libvirt ownership probes before context sweep"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "List libvirt domains for current-context sweep", action: "ansible.builtin.command"}:                                                                               {class: "probe", surface: "libvirt context sweep", anchor: "Require conclusive libvirt ownership probes before context sweep"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Remove current-context libvirt ownership records", action: "ansible.builtin.file"}:                                                                                {class: "evidence", surface: "libvirt context sweep", anchor: "Remove current-context libvirt ownership records"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Remove current-context libvirt storage directory", action: "ansible.builtin.file"}:                                                                                {class: "removal", surface: "libvirt context sweep", anchor: "Stop current-context libvirt domains"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Remove libvirt host packages introduced by Bootwright", action: "ansible.builtin.import_role"}:                                                                    {class: "removal", surface: "libvirt context sweep", anchor: "Stop current-context libvirt domains"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Remove owned current-context libvirt machine directories", action: "ansible.builtin.file"}:                                                                        {class: "removal", surface: "libvirt context sweep", anchor: "Stop current-context libvirt domains"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Stop current-context libvirt domains", action: "ansible.builtin.command"}:                                                                                         {class: "removal", surface: "libvirt context sweep", anchor: "Stop current-context libvirt domains"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Undefine current-context libvirt domains", action: "ansible.builtin.command"}:                                                                                     {class: "removal", surface: "libvirt context sweep", anchor: "Stop current-context libvirt domains"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/destroy_context.yml", name: "Verify swept current-context libvirt domains are absent", action: "ansible.builtin.command"}:                                                                      {class: "probe", surface: "libvirt context sweep", anchor: "Require conclusive libvirt ownership probes before context sweep"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/main.yml", name: "Install libvirt host packages", action: "ansible.builtin.include_role"}:                                                                                                      {class: "delegated", surface: "libvirt provider host apply", anchor: "Install libvirt host packages"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt/tasks/main.yml", name: "Start libvirt service", action: "ansible.builtin.service"}:                                                                                                                   {class: "mutation", surface: "libvirt provider host apply", anchor: "Install libvirt host packages"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/credentials.yml", name: "Converge BMC htpasswd", action: "ansible.builtin.include_role"}:                                                                                         {class: "delegated", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/credentials.yml", name: "Load BMC credentials", action: "ansible.builtin.include_role"}:                                                                                          {class: "probe", surface: "emulated BMC apply", anchor: "Prove BMC emulator ownership before apply mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition.yml", name: "Prove existing BMC composite before apply or transition", action: "ansible.builtin.include_role"}:                                              {class: "gate", surface: "emulated BMC apply", anchor: "Prove BMC emulator ownership before apply mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition.yml", name: "Restore desired BMC emulator facts after ownership transition", action: "ansible.builtin.import_tasks"}:                                        {class: "delegated", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition.yml", name: "Validate exact live BMC owner record before apply", action: "ansible.builtin.include_role"}:                                                    {class: "probe", surface: "emulated BMC apply", anchor: "Prove BMC emulator ownership before apply mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition_cleanup.yml", name: "Close obsolete recorded BMC Redfish firewall port", action: "ansible.posix.firewalld"}:                                                 {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition_cleanup.yml", name: "Remove obsolete recorded BMC libvirt vmedia pool", action: "community.libvirt.virt_pool"}:                                              {class: "removal", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition_cleanup.yml", name: "Verify obsolete recorded BMC pool absence", action: "ansible.builtin.command"}:                                                         {class: "probe", surface: "emulated BMC apply", anchor: "Prove BMC emulator ownership before apply mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Apply SELinux labels to venv binaries", action: "ansible.builtin.command"}:                                                                                 {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Create BMC emulator temp directory", action: "ansible.builtin.file"}:                                                                                       {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Create BMC state directories", action: "ansible.builtin.file"}:                                                                                             {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Create sushy-tools virtualenv", action: "ansible.builtin.command"}:                                                                                         {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Ensure SELinux file context labels venv binaries as bin_t", action: "community.general.sefcontext"}:                                                        {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Install BMC packages", action: "ansible.builtin.package"}:                                                                                                  {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Install sushy-tools into virtualenv", action: "ansible.builtin.pip"}:                                                                                       {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Restore BMC vmedia storage labels", action: "ansible.builtin.command"}:                                                                                     {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Activate libvirt vmedia storage pool", action: "community.libvirt.virt_pool"}:                                                                                 {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Define libvirt vmedia storage pool", action: "community.libvirt.virt_pool"}:                                                                                   {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Enable autostart on libvirt vmedia storage pool", action: "community.libvirt.virt_pool"}:                                                                      {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Ensure sushy-emulator service is running", action: "ansible.builtin.systemd_service"}:                                                                         {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Open Redfish port on host firewall", action: "ansible.posix.firewalld"}:                                                                                       {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Reload systemd manager when sushy unit changed", action: "ansible.builtin.systemd_service"}:                                                                   {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Wait for sushy-emulator Redfish endpoint", action: "ansible.builtin.uri"}:                                                                                     {class: "probe", surface: "emulated BMC apply", anchor: "Prove BMC emulator ownership before apply mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Write sushy-emulator config", action: "ansible.builtin.template"}:                                                                                             {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Write sushy-emulator systemd unit", action: "ansible.builtin.template"}:                                                                                       {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/vmedia.yml", name: "Ensure vmedia HTTP service is running", action: "ansible.builtin.systemd_service"}:                                                                           {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/vmedia.yml", name: "Reload systemd when vmedia unit changed", action: "ansible.builtin.systemd_service"}:                                                                         {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/vmedia.yml", name: "Write vmedia HTTP systemd unit", action: "ansible.builtin.template"}:                                                                                         {class: "mutation", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Destroy exact claimed partial BMC composite", action: "ansible.builtin.include_role"}:                                                                             {class: "removal", surface: "emulated BMC destroy", anchor: "Destroy exact claimed partial BMC composite"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Destroy exact recorded BMC composite and ownership", action: "ansible.builtin.include_role"}:                                                                      {class: "removal", surface: "emulated BMC destroy", anchor: "Destroy exact recorded BMC composite and ownership"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Prove no unclaimed BMC emulator survives desired destroy", action: "ansible.builtin.import_tasks"}:                                                                {class: "gate", surface: "emulated BMC destroy", anchor: "Prove recorded BMC emulator ownership before desired destroy mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Prove recorded BMC emulator ownership before desired destroy mutation", action: "ansible.builtin.include_role"}:                                                   {class: "gate", surface: "emulated BMC destroy", anchor: "Prove recorded BMC emulator ownership before desired destroy mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Resolve BMC destroy facts", action: "ansible.builtin.import_tasks"}:                                                                                               {class: "removal", surface: "emulated BMC destroy", anchor: "Destroy exact recorded BMC composite and ownership"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Validate live BMC owner record before desired teardown", action: "ansible.builtin.include_role"}:                                                                  {class: "probe", surface: "emulated BMC destroy", anchor: "Prove recorded BMC emulator ownership before desired destroy mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Configure BMC emulator credentials", action: "ansible.builtin.import_tasks"}:                                                                                         {class: "delegated", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Configure sushy-emulator", action: "ansible.builtin.import_tasks"}:                                                                                                   {class: "delegated", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Configure vmedia HTTP service", action: "ansible.builtin.import_tasks"}:                                                                                              {class: "delegated", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Prepare BMC emulator host", action: "ansible.builtin.import_tasks"}:                                                                                                  {class: "delegated", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Prove BMC emulator ownership before apply mutation", action: "ansible.builtin.import_tasks"}:                                                                         {class: "gate", surface: "emulated BMC apply", anchor: "Prove BMC emulator ownership before apply mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Reconcile exact recorded BMC composite before apply", action: "ansible.builtin.import_tasks"}:                                                                        {class: "delegated", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Record BMC emulator ownership", action: "ansible.builtin.include_role"}:                                                                                              {class: "evidence", surface: "emulated BMC apply", anchor: "Record BMC emulator ownership"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Resolve BMC emulator facts", action: "ansible.builtin.import_tasks"}:                                                                                                 {class: "probe", surface: "emulated BMC apply", anchor: "Prove BMC emulator ownership before apply mutation"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Retire obsolete recorded BMC members after desired runtime success", action: "ansible.builtin.import_tasks"}:                                                         {class: "removal", surface: "emulated BMC apply", anchor: "Prepare BMC emulator host"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/ownership_gate.yml", name: "Probe BMC libvirt vmedia pool XML", action: "ansible.builtin.command"}:                                                                                     {class: "probe", surface: "emulated BMC apply", anchor: "Prove BMC emulator ownership before apply mutation"},
}

var providerMutationTaskSupplement = []providerMutationTask{
	{path: "ansible/collections/ansible_collections/bootwright/core/playbooks/task_host_shared_service_operation_finalize.yml", name: "Finalize host-wide shared-service operation proof", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/playbooks/task_host_shared_service_operation_finalize.yml", name: "Release this command's exact host-wide shared-service operation", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_claim.yml", name: "Atomically acquire exact host endpoint claim", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_claim.yml", name: "Revalidate host operation before endpoint acquisition", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_claims.yml", name: "Acquire exact host endpoint slots", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_claims.yml", name: "Reserve complete host endpoint consequence set", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_slots.yml", name: "Acquire one exact host endpoint claim", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_slots.yml", name: "Revalidate host operation before endpoint slot acquisition", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_shared_service_operation.yml", name: "Acquire unique host-wide shared-service operation guard atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_shared_service_operation.yml", name: "Resolve expected host-wide shared-service operation guard", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_infra_component_endpoint_claims.yml", name: "Acquire sorted infra-component endpoint slots after family recovery publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_infra_component_endpoint_claims.yml", name: "Reserve complete infra-component endpoint registry after family recovery", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_infra_component_global_claim.yml", name: "Publish exact host-global infra-component transition atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_infra_component_global_claim.yml", name: "Revalidate exclusive host operation before global transition publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_destroy_transition.yml", name: "Acquire every destroy-side endpoint before external mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_destroy_transition.yml", name: "Bind infra-component destroy transition to command selection", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_destroy_transition.yml", name: "Preflight every shared-service consequence before infra destroy publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_destroy_transition.yml", name: "Publish authoritative host-global infra-component destroying claim atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_destroy_transition.yml", name: "Refuse collisions before infra-component destroy transition publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_destroy_transition.yml", name: "Require exclusive host operation before destroy transition publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Acquire every pending endpoint after durable transition recovery publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Bind infra-component apply transition to command selection", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Preflight every shared-service consequence before infra publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Publish authoritative host-global transition before local recovery evidence", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Publish durable infra-component transition atomically before role mutation", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Publish infra-component transition recovery ownership record", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Read current infra-component owner before apply transition", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Read durable infra-component transition recovery record", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Refuse collisions with durable infra-component host consequences", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml", name: "Require exclusive host operation before any transition publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/bind_infra_component_host_operation.yml", name: "Revalidate host operation before infra-component selection binding", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml", name: "Persist completed infra-component external cleanup", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml", name: "Remove exact infra-component cleanup-phase owner last", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml", name: "Remove final context-marked infra-component state directory", action: "ansible.builtin.file"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml", name: "Retire exact host-global infra-component destroy authority", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml", name: "Retire exact local infra-component destroy transition", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml", name: "Revalidate host operation before cleanup phase publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml", name: "Revalidate host operation before final controller owner removal", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml", name: "Revalidate host operation before final state removal", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Clear exact role-specific completion boundary before transition settlement", action: "ansible.builtin.file"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Read completed infra-component transition recovery record", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Retire completed durable local infra-component transition atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Retire exact infra-component transition recovery record", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Retire old-only infra-component endpoint claims after external cleanup", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Revalidate completed infra-component owner record", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Revalidate host operation before local transition evidence retirement", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Revalidate host operation before role-specific completion boundary cleanup", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml", name: "Settle authoritative host-global infra-component claim last in this role", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_record_gate.yml", name: "Probe firewalld before recorded BMC teardown classification", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Acquire exact BMC endpoint authority for recorded or claim-backed teardown", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Destroy exact BMC provider-global composite", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_resource.yml", name: "Revalidate host operation before BMC owner record removal", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Acquire service claim atomically before mutation for {{ bootwright_component.name }}", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Normalize service claim directory after exact claim for {{ bootwright_component.name }}", action: "ansible.builtin.file"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Probe host-global claim for {{ bootwright_component.name }}", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Publish exact transition before service claim for {{ bootwright_component.name }}", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml", name: "Revalidate host operation before service claim for {{ bootwright_component.name }}", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_destroy_gate.yml", name: "Begin authoritative infra-component destroy transition", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/mark_infra_component_external_cleanup.yml", name: "Publish claim-only infra-component cleanup recovery record", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/mark_infra_component_external_cleanup.yml", name: "Publish completed infra-component external cleanup boundary", action: "ansible.builtin.copy"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/mark_infra_component_external_cleanup.yml", name: "Revalidate infra-component cleanup recovery ownership", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/mark_infra_component_external_cleanup.yml", name: "Revalidate infra-component ownership before cleanup phase publication", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/preflight_host_shared_service_consequences.yml", name: "Revalidate host operation after shared-service consequence preflight", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/preflight_host_shared_service_consequences.yml", name: "Revalidate host operation before shared-service consequence preflight", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_endpoint_claim.yml", name: "Atomically release exact host endpoint claim", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_endpoint_claim.yml", name: "Revalidate host operation before endpoint release", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_endpoint_claims.yml", name: "Atomically release full host endpoint reservation", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_endpoint_claims.yml", name: "Release grouped exact host endpoint claims", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_endpoint_claims.yml", name: "Revalidate host operation before full endpoint reservation release", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_shared_service_operation.yml", name: "Resolve expected host-wide operation for finalization", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_host_endpoint_claim.yml", name: "Revalidate host operation before endpoint recheck", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_host_endpoint_claims.yml", name: "Recheck every exact host endpoint claim", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_host_endpoint_claims.yml", name: "Revalidate host operation before full endpoint reservation recheck", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_host_shared_service_operation.yml", name: "Resolve expected host-wide operation for revalidation", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_infra_component_endpoint_claims.yml", name: "Rebind infra-component endpoint mutation to command selection", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_infra_component_endpoint_claims.yml", name: "Require every exact infra-component endpoint claim", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_infra_component_global_consequences.yml", name: "Revalidate exclusive host operation after consequence scan", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_infra_component_global_consequences.yml", name: "Revalidate exclusive host operation before consequence scan", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/reserve_host_endpoint_claims.yml", name: "Publish complete host endpoint reservation atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/reserve_host_endpoint_claims.yml", name: "Revalidate host operation before full endpoint reservation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/reserve_infra_component_endpoint_claims.yml", name: "Build stable endpoint consequences for every transition side", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/reserve_infra_component_endpoint_claims.yml", name: "Reserve complete infra-component endpoint union atomically", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_destroy_transition.yml", name: "Remove exact infra-component transition recovery ownership", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_destroy_transition.yml", name: "Retire exact infra-component destroy transition claim atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_destroy_transition.yml", name: "Revalidate host operation before local transition claim retirement", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_destroy_transition.yml", name: "Revalidate host operation before transition recovery retirement", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_endpoint_claims.yml", name: "Release exact old-only infra-component endpoint claims", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_global_claim.yml", name: "Retire exact host-global infra-component destroying claim atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_global_claim.yml", name: "Retire exact infra-component endpoint authority after external cleanup", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_global_claim.yml", name: "Revalidate exact infra-component endpoint authority before retirement", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_global_claim.yml", name: "Revalidate host operation before endpoint authority retirement", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_global_claim.yml", name: "Revalidate host operation before global claim retirement", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/rollback_host_endpoint_claim.yml", name: "Revalidate host operation before endpoint rollback", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/rollback_host_endpoint_claim.yml", name: "Roll back exact host endpoint claim atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/settle_infra_component_global_claim.yml", name: "Revalidate host operation before settling infra-component claim", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/settle_infra_component_global_claim.yml", name: "Settle authoritative host-global infra-component claim atomically", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/credentials.yml", name: "Revalidate BMC authority before credential file mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/endpoint_claims.yml", name: "Bind BMC apply to command-wide selected host consequences", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/endpoint_claims.yml", name: "Preflight symmetric host consequences before BMC apply publication", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/endpoint_reservation.yml", name: "Reserve complete active and pending BMC endpoint consequence set", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/endpoint_slots.yml", name: "Acquire active and pending BMC endpoint slots after durable claim publication", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition.yml", name: "Probe exact BMC provider-global claim before apply classification", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition_cleanup.yml", name: "Release obsolete exact BMC endpoint claims after cleanup proof", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition_cleanup.yml", name: "Revalidate BMC authority before old firewall mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition_cleanup.yml", name: "Revalidate BMC authority before old pool mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Revalidate BMC authority before Python package mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Revalidate BMC authority before SELinux policy mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Revalidate BMC authority before package mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Revalidate BMC authority before state directory mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Revalidate BMC authority before temp directory mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Revalidate BMC authority before venv label mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Revalidate BMC authority before virtualenv mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml", name: "Revalidate BMC authority before vmedia label mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/port_transition.yml", name: "Revalidate BMC authority before cross-port Redfish stop", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/port_transition.yml", name: "Revalidate BMC authority before cross-port vmedia stop", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/port_transition.yml", name: "Stop old Redfish unit blocking desired vmedia port", action: "ansible.builtin.systemd_service"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/port_transition.yml", name: "Stop old vmedia unit blocking desired Redfish port", action: "ansible.builtin.systemd_service"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Revalidate BMC authority before Redfish firewall mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Revalidate BMC authority before pool activation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Revalidate BMC authority before pool autostart mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Revalidate BMC authority before pool definition", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Revalidate BMC authority before sushy config mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Revalidate BMC authority before sushy daemon reload", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Revalidate BMC authority before sushy service mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml", name: "Revalidate BMC authority before sushy unit mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/vmedia.yml", name: "Revalidate BMC authority before vmedia daemon reload", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/vmedia.yml", name: "Revalidate BMC authority before vmedia service mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/vmedia.yml", name: "Revalidate BMC authority before vmedia unit mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/claim_probe.yml", name: "List BMC provider-global claim root entries", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml", name: "Atomically advance BMC full claim to desired state", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml", name: "Atomically complete exact BMC transition", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml", name: "Re-probe BMC claim before ownership completion", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml", name: "Re-probe advanced BMC full claim", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml", name: "Re-probe completed BMC claim", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml", name: "Revalidate BMC authority before claim completion", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml", name: "Revalidate BMC authority before transition completion", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml", name: "Revalidate BMC owner record before claim completion", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Probe exact BMC provider-global claim before unrecorded destroy", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Probe firewalld before BMC teardown classification", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Remove positively empty BMC provider-global claim root", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml", name: "Revalidate host operation before empty BMC claim-root recovery", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_endpoint_claims.yml", name: "Acquire exact BMC endpoint authority for resumable destroy", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_endpoint_claims.yml", name: "Bind BMC destroy to command-wide selected host consequences", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_endpoint_claims.yml", name: "Preflight symmetric host consequences before BMC destroy authority", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_mutation_guard.yml", name: "Re-probe exact BMC destroy evidence before mutation", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_mutation_guard.yml", name: "Revalidate exact BMC endpoint claims before destroy mutation", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_mutation_guard.yml", name: "Revalidate host-wide operation before BMC destroy mutation", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Atomically remove exact BMC full claim after teardown", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Atomically remove exact BMC transition after teardown", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Atomically remove exact legacy BMC marker after teardown", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Close exact recorded BMC Redfish firewall port", action: "ansible.posix.firewalld"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Probe removed BMC systemd units", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Re-probe BMC claim immediately before evidence removal", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Re-probe exact BMC libvirt pool immediately before removal", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Re-probe mounts beneath BMC roots immediately before path removal", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Re-prove BMC claim units mounts and pool immediately before teardown", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Recheck BMC ownership record immediately before teardown", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Recheck BMC record immediately before evidence removal", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Recheck claimed partial BMC record absence after claim removal", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Release all exact BMC endpoint claims after teardown proof", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Reload systemd after exact BMC unit removal", action: "ansible.builtin.systemd_service"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Remove empty BMC provider-global claim root", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Remove exact BMC libvirt vmedia pool", action: "community.libvirt.virt_pool"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Remove exact BMC runtime paths while retaining durable claim", action: "ansible.builtin.file"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate BMC destroy authority before daemon reload", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate BMC destroy authority before endpoint evidence removal", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate BMC destroy authority before firewall mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate BMC destroy authority before recorded pool mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate BMC destroy authority before runtime path mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate BMC destroy authority before unit mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate host operation before BMC full claim removal", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate host operation before BMC transition removal", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate host operation before empty BMC claim root removal", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Revalidate host operation before legacy BMC marker removal", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Stop exact loaded BMC systemd units", action: "ansible.builtin.systemd_service"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Tear down both exact BMC transition consequences", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml", name: "Verify exact BMC libvirt vmedia pool absence", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_transition.yml", name: "Close exact BMC transition firewall ports", action: "ansible.posix.firewalld"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_transition.yml", name: "Re-probe every BMC transition pool immediately before removal", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_transition.yml", name: "Remove exact BMC transition pools", action: "community.libvirt.virt_pool"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_transition.yml", name: "Revalidate BMC destroy authority before transition firewall mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_transition.yml", name: "Revalidate BMC destroy authority before transition pool mutation", action: "ansible.builtin.include_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_transition.yml", name: "Verify every BMC transition pool is absent", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Acquire exact BMC endpoint slots after durable claim publication", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Complete host-atomic BMC claim after ownership success", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Publish host-atomic BMC claim before apply mutation", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Release old listeners blocking desired BMC cross-port transition", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Reserve complete BMC endpoint union after durable claim publication", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Resolve exact BMC endpoints before durable intent publication", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Revalidate BMC authority before owner record mutation", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml", name: "Revalidate host-wide operation before BMC evidence publication", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/mutation_guard.yml", name: "Re-probe exact BMC claim transition before mutation", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/mutation_guard.yml", name: "Revalidate exact BMC endpoint claims before mutation", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/mutation_guard.yml", name: "Revalidate host-wide operation before BMC mutation", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/ownership_gate.yml", name: "Probe exact BMC provider-global claim before live classification", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/ownership_gate.yml", name: "Probe loaded BMC systemd unit identities", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/ownership_gate.yml", name: "Probe mounts beneath BMC owned roots", action: "ansible.builtin.command"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml", name: "Atomically acquire or advance BMC full claim", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml", name: "Atomically publish BMC transition envelope before pending mutation", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml", name: "Atomically publish legacy BMC context marker", action: "bootwright.core.claim_cas"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml", name: "Re-probe BMC full claim winner", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml", name: "Re-probe BMC transition CAS winner", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml", name: "Re-probe atomic BMC claim winner before mutation", action: "ansible.builtin.import_tasks"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml", name: "Revalidate host operation before BMC marker publication", action: "ansible.builtin.include_role"},
	{path: "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml", name: "Revalidate host operation before BMC transition publication", action: "ansible.builtin.include_role"},
}

func init() {
	for _, ref := range providerMutationTaskSupplement {
		if _, found := providerMutationTasks[ref]; found {
			panic(fmt.Sprintf("duplicate provider mutation task registration: %#v", ref))
		}
		providerMutationTasks[ref] = providerMutationSupplementRegistration(ref)
	}
}

func providerMutationSupplementRegistration(ref providerMutationTask) providerMutationTaskRegistration {
	surface := "infra-component apply authority"
	gateAnchor := "Refuse collisions with durable infra-component host consequences"
	mutationAnchor := "Publish authoritative host-global transition before local recovery evidence"
	evidenceAnchor := ""

	switch {
	case strings.Contains(ref.path, "/provider_service_bmc_emulated/tasks/destroy") ||
		strings.HasSuffix(ref.path, "/ownership_record/tasks/destroy_record_gate.yml") ||
		strings.HasSuffix(ref.path, "/ownership_record/tasks/destroy_resource.yml"):
		surface = "emulated BMC destroy"
		gateAnchor = "Prove recorded BMC emulator ownership before desired destroy mutation"
		mutationAnchor = "Destroy exact recorded BMC composite and ownership"
	case strings.Contains(ref.path, "/provider_service_bmc_emulated/"):
		surface = "emulated BMC apply"
		gateAnchor = "Prove BMC emulator ownership before apply mutation"
		mutationAnchor = "Prepare BMC emulator host"
		evidenceAnchor = "Record BMC emulator ownership"
	case strings.HasSuffix(ref.path, "/task_host_shared_service_operation_finalize.yml") ||
		strings.HasSuffix(ref.path, "/release_host_shared_service_operation.yml"):
		surface = "host shared-service operation finalization"
		gateAnchor = "Require absent or exact host-wide operation before finalization"
		mutationAnchor = "Atomically finalize exact host-wide shared-service operation"
	case strings.HasSuffix(ref.path, "/acquire_host_shared_service_operation.yml"):
		surface = "host shared-service operation acquisition"
		gateAnchor = "Initialize host-wide shared-service operation acquisition"
		mutationAnchor = "Acquire unique host-wide shared-service operation guard atomically"
	case strings.Contains(ref.path, "complete_infra_component_destroy") ||
		strings.Contains(ref.path, "mark_infra_component_external_cleanup") ||
		strings.Contains(ref.path, "retire_infra_component_destroy") ||
		strings.Contains(ref.path, "retire_infra_component_global"):
		surface = "infra-component destroy completion"
		gateAnchor = "Revalidate host operation before cleanup phase publication"
		mutationAnchor = "Persist completed infra-component external cleanup"
		evidenceAnchor = "Remove exact infra-component cleanup-phase owner last"
	case strings.Contains(ref.path, "begin_infra_component_destroy") ||
		strings.HasSuffix(ref.path, "/infra_component_destroy_gate.yml"):
		surface = "infra-component destroy authority"
		gateAnchor = "Refuse collisions before infra-component destroy transition publication"
		mutationAnchor = "Publish authoritative host-global infra-component destroying claim atomically"
	case strings.Contains(ref.path, "complete_infra_component_transition") ||
		strings.Contains(ref.path, "settle_infra_component_global") ||
		strings.Contains(ref.path, "retire_infra_component_endpoint"):
		surface = "infra-component apply completion"
		gateAnchor = "Require exact role-specific completion boundary before cleanup"
		mutationAnchor = "Clear exact role-specific completion boundary before transition settlement"
	case strings.Contains(ref.path, "host_endpoint") ||
		strings.Contains(ref.path, "endpoint_claim"):
		surface = "host endpoint authority"
		gateAnchor = "Refuse conflicting atomic host endpoint reservation"
		mutationAnchor = "Publish complete host endpoint reservation atomically"
	}

	class := "delegated"
	lowerName := strings.ToLower(ref.name)
	gatePrefix := strings.HasPrefix(lowerName, "bind ") ||
		strings.HasPrefix(lowerName, "classify ") ||
		strings.HasPrefix(lowerName, "inspect ") ||
		strings.HasPrefix(lowerName, "list ") ||
		strings.HasPrefix(lowerName, "preflight ") ||
		strings.HasPrefix(lowerName, "probe ") ||
		strings.HasPrefix(lowerName, "read ") ||
		strings.HasPrefix(lowerName, "re-probe ") ||
		strings.HasPrefix(lowerName, "re-read ") ||
		strings.HasPrefix(lowerName, "require ") ||
		strings.HasPrefix(lowerName, "resolve ") ||
		strings.HasPrefix(lowerName, "refuse ") ||
		strings.HasPrefix(lowerName, "revalidate ")
	removalName := strings.Contains(lowerName, "cleanup") ||
		strings.HasPrefix(lowerName, "clear ") ||
		strings.HasPrefix(lowerName, "destroy ") ||
		strings.HasPrefix(lowerName, "release ") ||
		strings.HasPrefix(lowerName, "remove ") ||
		strings.HasPrefix(lowerName, "retire ") ||
		strings.HasPrefix(lowerName, "roll back ") ||
		strings.HasPrefix(lowerName, "stop ") ||
		strings.HasPrefix(lowerName, "tear down ")

	switch ref.action {
	case "ansible.builtin.include_role", "ansible.builtin.include_tasks", "ansible.builtin.import_role", "ansible.builtin.import_tasks":
		if gatePrefix {
			class = "gate"
		} else if removalName {
			class = "removal"
		}
	case "ansible.builtin.command":
		if gatePrefix || strings.HasPrefix(lowerName, "verify ") {
			class = "probe"
		} else if removalName {
			class = "removal"
		} else {
			class = "mutation"
		}
	case "bootwright.core.claim_cas":
		if removalName || strings.Contains(lowerName, "finalize") || strings.Contains(lowerName, "complete exact") {
			class = "removal"
		} else {
			class = "mutation"
		}
	case "ansible.builtin.file":
		if removalName {
			class = "removal"
		} else {
			class = "mutation"
		}
	default:
		if removalName || strings.HasPrefix(lowerName, "close ") {
			class = "removal"
		} else {
			class = "mutation"
		}
	}

	anchor := mutationAnchor
	if class == "gate" || class == "probe" {
		anchor = gateAnchor
	}
	if strings.Contains(lowerName, "owner last") && evidenceAnchor != "" {
		class = "evidence"
		anchor = evidenceAnchor
	}
	return providerMutationTaskRegistration{class: class, surface: surface, anchor: anchor}
}

var providerMutationTaskFiles = map[string]string{
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_claim.yml":                 "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_claims.yml":                "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_endpoint_slots.yml":                 "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_host_shared_service_operation.yml":       "host shared-service operation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_infra_component_endpoint_claims.yml":     "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/acquire_infra_component_global_claim.yml":        "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_destroy_transition.yml":    "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml":            "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/bind_infra_component_host_operation.yml":         "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml":            "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml":         "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/mark_infra_component_external_cleanup.yml":       "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/preflight_host_shared_service_consequences.yml":  "host shared-service operation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_endpoint_claim.yml":                 "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/release_host_endpoint_claims.yml":                "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_host_endpoint_claim.yml":                 "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_host_endpoint_claims.yml":                "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_host_shared_service_operation.yml":       "host shared-service operation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_infra_component_endpoint_claims.yml":     "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/require_infra_component_global_consequences.yml": "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/reserve_host_endpoint_claims.yml":                "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/reserve_infra_component_endpoint_claims.yml":     "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_destroy_transition.yml":   "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_endpoint_claims.yml":      "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/retire_infra_component_global_claim.yml":         "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/rollback_host_endpoint_claim.yml":                "host endpoint mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/settle_infra_component_global_claim.yml":         "infra-component mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/endpoint_claims.yml":          "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/endpoint_reservation.yml":     "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/endpoint_slots.yml":           "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/port_transition.yml":          "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/claim_probe.yml":                    "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/complete_claim.yml":                 "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_endpoint_claims.yml":        "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_mutation_guard.yml":         "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_owned.yml":                  "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy_transition.yml":             "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/mutation_guard.yml":                 "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/publish_claim.yml":                  "emulated BMC mutation authority",
	"ansible/collections/ansible_collections/bootwright/core/playbooks/task_host_shared_service_operation_finalize.yml":                    "host shared-service operation finalizer dispatch",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_kubevirt/tasks/main.yml":                                                       "kubevirt boot media",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_kubevirt/tasks/purge_media_pvc.yml":                                            "kubevirt boot media removal",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/boot/media_insert.yml":                                           "redfish media insertion",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/boot/post_boot.yml":                                              "redfish post-boot mutation",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/boot/power_override.yml":                                         "redfish power override",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/boot/power_state_probe.yml":                                      "redfish power probe",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/main.yml":                                                        "redfish boot dispatch",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/eject.yml":                                                 "redfish media ejection",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/import_certificate.yml":                                    "redfish certificate mutation",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/import_certificate/security_service.yml":                   "redfish security-service certificate mutation",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/import_certificate/standard.yml":                           "redfish standard certificate mutation",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/insert_attempt.yml":                                        "redfish media mutation",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/prepare.yml":                                               "redfish media preparation",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/remove_certificate/security_service.yml":                   "redfish security-service certificate removal",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/remove_certificate/standard.yml":                           "redfish standard certificate removal",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/media/restore_certificate_verification.yml":                      "redfish certificate-policy restoration",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/stage/system.yml":                                                "redfish system staging",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/validation/macs.yml":                                             "redfish MAC probe",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/validation/power.yml":                                            "redfish power probe",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/validation/tpm2.yml":                                             "redfish TPM probe",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_vsphere/tasks/power.yml":                                                       "vsphere boot power",
	bootwrightCollectionRoleRoot + "/container_cluster_boot_vsphere/tasks/stage_agent_iso.yml":                                             "vsphere boot media staging",
	bootwrightCollectionRoleRoot + "/container_cluster_media_libvirt/tasks/eject.yml":                                                      "libvirt media removal",
	bootwrightCollectionRoleRoot + "/container_cluster_media_libvirt/tasks/main.yml":                                                       "libvirt media mutation",
	bootwrightCollectionRoleRoot + "/container_cluster_media_vsphere/tasks/cleanup.yml":                                                    "vsphere media removal",
	bootwrightCollectionRoleRoot + "/container_cluster_media_vsphere/tasks/insert.yml":                                                     "vsphere media mutation",
	bootwrightCollectionRoleRoot + "/machine_substrate_baremetal/tasks/destroy.yml":                                                        "baremetal local evidence removal",
	bootwrightCollectionRoleRoot + "/machine_substrate_baremetal/tasks/main.yml":                                                           "baremetal managed install",
	bootwrightCollectionRoleRoot + "/machine_substrate_kubevirt/tasks/destroy.yml":                                                         "kubevirt substrate removal",
	bootwrightCollectionRoleRoot + "/machine_substrate_kubevirt/tasks/main.yml":                                                            "kubevirt substrate mutation",
	bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/destroy.yml":                                                          "libvirt substrate removal",
	bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/machine.yml":                                                          "libvirt substrate mutation",
	bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/network.yml":                                                          "libvirt network mutation",
	bootwrightCollectionRoleRoot + "/machine_substrate_vsphere/tasks/apply.yml":                                                            "vsphere substrate mutation",
	bootwrightCollectionRoleRoot + "/machine_substrate_vsphere/tasks/destroy.yml":                                                          "vsphere substrate removal",
	bootwrightCollectionRoleRoot + "/machine_substrate_vsphere/tasks/destroy_vmedia.yml":                                                   "vsphere media removal",
	bootwrightCollectionRoleRoot + "/machine_substrate_vsphere/tasks/probe.yml":                                                            "vsphere identity gate",
	bootwrightCollectionRoleRoot + "/provider_host_libvirt/tasks/destroy.yml":                                                              "libvirt provider host removal",
	bootwrightCollectionRoleRoot + "/provider_host_libvirt/tasks/destroy_context.yml":                                                      "libvirt context sweep",
	bootwrightCollectionRoleRoot + "/provider_host_libvirt/tasks/main.yml":                                                                 "libvirt provider host",
	bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/apply/ownership_transition_cleanup.yml":                           "emulated BMC ownership transition cleanup",
	bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/apply/packages.yml":                                               "emulated BMC package mutation",
	bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/apply/sushy.yml":                                                  "emulated BMC service mutation",
	bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/apply/vmedia.yml":                                                 "emulated BMC media mutation",
	bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/ownership_gate.yml":                                               "emulated BMC identity gate",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/destroy_record_libvirt_gate.yml":                                               "recorded libvirt identity gate",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/destroy_resource.yml":                                                          "recorded provider resource removal",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/infra_component_container_gate.yml":                                            "container identity gate",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/infra_component_destroy_gate.yml":                                              "component identity gate",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/package_apply.yml":                                                             "owned package mutation",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/package_remove_one.yml":                                                        "owned package removal",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/package_records_write.yml":                                                     "package ownership evidence mutation",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/remove_resource.yml":                                                           "ownership evidence removal",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/release_host_shared_service_operation.yml":                                     "host shared-service operation finalization",
	bootwrightCollectionRoleRoot + "/ownership_record/tasks/resource.yml":                                                                  "ownership evidence mutation",
	"ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/prepare_destroy_cluster.yml":                    "libvirt network removal gate",
	"ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/remove_libvirt_network.yml":                     "libvirt network removal",
	"ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/destroy_machine.yml":                            "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power.yml":                    "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_wait.yml":         "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_until_attached.yml":   "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/remove_certificate.yml":      "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/stage/credentials.yml":             "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/cleanup_media.yml":                 "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/main.yml":                          "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/power_gate.yml":                    "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_vsphere/tasks/prepare_media.yml":                 "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/context.yml":                      "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_vsphere/tasks/main.yml":                         "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/context.yml":                            "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/main.yml":                               "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_vsphere/tasks/ownership.yml":                          "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_declared_managed_os.yml":                 "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/destroy_record_gate.yml":                         "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_apply_one.yml":                           "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/package_remove.yml":                              "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/credentials.yml":              "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/ownership_transition.yml":     "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml":                        "provider mutation surface",
	"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/main.yml":                           "provider mutation surface",
}

var providerMutationSurfaces = []providerMutationSurface{
	{
		name:      "host shared-service operation acquisition",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/acquire_host_shared_service_operation.yml",
		gates:     []string{"Initialize host-wide shared-service operation acquisition"},
		mutations: []string{"Acquire unique host-wide shared-service operation guard atomically"},
	},
	{
		name:      "host endpoint authority",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/reserve_host_endpoint_claims.yml",
		gates:     []string{"Refuse conflicting atomic host endpoint reservation"},
		mutations: []string{"Publish complete host endpoint reservation atomically"},
	},
	{
		name:      "infra-component apply authority",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/begin_infra_component_transition.yml",
		gates:     []string{"Refuse collisions with durable infra-component host consequences"},
		mutations: []string{"Publish authoritative host-global transition before local recovery evidence"},
	},
	{
		name:      "infra-component apply completion",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/complete_infra_component_transition.yml",
		gates:     []string{"Require exact role-specific completion boundary before cleanup"},
		mutations: []string{"Clear exact role-specific completion boundary before transition settlement"},
	},
	{
		name:      "infra-component destroy authority",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/begin_infra_component_destroy_transition.yml",
		gates:     []string{"Refuse collisions before infra-component destroy transition publication"},
		mutations: []string{"Publish authoritative host-global infra-component destroying claim atomically"},
	},
	{
		name:      "infra-component destroy completion",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/complete_infra_component_destroy.yml",
		gates:     []string{"Revalidate host operation before cleanup phase publication"},
		mutations: []string{"Persist completed infra-component external cleanup"},
		evidence:  []string{"Remove exact infra-component cleanup-phase owner last"},
	},
	{
		name:      "host shared-service operation finalization",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/release_host_shared_service_operation.yml",
		gates:     []string{"Require absent or exact host-wide operation before finalization"},
		mutations: []string{"Atomically finalize exact host-wide shared-service operation"},
	},
	{
		name:      "libvirt provider host apply",
		provider:  "libvirt",
		path:      bootwrightCollectionRoleRoot + "/provider_host_libvirt/tasks/main.yml",
		gates:     []string{"Load libvirt package list"},
		mutations: []string{"Install libvirt host packages", "Start libvirt service"},
	},
	{
		name:      "libvirt provider host destroy",
		provider:  "libvirt",
		path:      bootwrightCollectionRoleRoot + "/provider_host_libvirt/tasks/destroy.yml",
		gates:     []string{"Load libvirt package list"},
		mutations: []string{"Remove libvirt packages introduced by Bootwright"},
	},
	{
		name:     "emulated BMC apply",
		provider: "libvirt",
		path:     bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/main.yml",
		gates:    []string{"Prove BMC emulator ownership before apply mutation"},
		mutations: []string{
			"Prepare BMC emulator host",
			"Configure BMC emulator credentials",
			"Configure sushy-emulator",
			"Configure vmedia HTTP service",
			"Retire obsolete recorded BMC members after desired runtime success",
		},
		evidence: []string{"Record BMC emulator ownership"},
	},
	{
		name:     "emulated BMC destroy",
		provider: "libvirt",
		path:     bootwrightCollectionRoleRoot + "/provider_service_bmc_emulated/tasks/destroy.yml",
		gates: []string{
			"Prove recorded BMC emulator ownership before desired destroy mutation",
			"Prove no unclaimed BMC emulator survives desired destroy",
			"Refuse unrecorded BMC live or local state",
		},
		mutations: []string{"Destroy exact claimed partial BMC composite", "Destroy exact recorded BMC composite and ownership"},
	},
	{
		name:      "baremetal substrate apply",
		provider:  "baremetal",
		path:      bootwrightCollectionRoleRoot + "/machine_substrate_baremetal/tasks/main.yml",
		gates:     []string{"Resolve baremetal manifest paths"},
		mutations: []string{"Create baremetal state directory"},
		evidence:  []string{"Record baremetal machine manifest"},
	},
	{
		name:      "baremetal substrate destroy",
		provider:  "baremetal",
		path:      bootwrightCollectionRoleRoot + "/machine_substrate_baremetal/tasks/destroy.yml",
		gates:     []string{"Resolve managed bare-metal install record"},
		mutations: []string{"Remove managed OS install artifacts", "Remove per-cluster OS-install state after the last machine", "Remove cluster baremetal state directory (idempotent)"},
	},
	{
		name:      "kubevirt boot media lifecycle",
		provider:  "kubevirt",
		path:      bootwrightCollectionRoleRoot + "/container_cluster_boot_kubevirt/tasks/main.yml",
		gates:     []string{"Enforce KubeVirt agent ISO DataVolume apply mode"},
		mutations: []string{"Upgrade legacy KubeVirt agent ISO DataVolume ownership labels"},
	},
	{
		name:      "redfish boot lifecycle",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/container_cluster_boot_redfish/tasks/main.yml",
		gates:     []string{"Prove required TPM 2.0 before Redfish mutation", "Require a powered-off machine before installer boot"},
		mutations: []string{"Prepare Redfish virtual media", "Boot node from Redfish virtual media"},
	},
	{
		name:      "vsphere boot lifecycle",
		provider:  "vsphere",
		path:      bootwrightCollectionRoleRoot + "/container_cluster_boot_vsphere/tasks/main.yml",
		gates:     []string{"Require a powered-off machine before installer boot"},
		mutations: []string{"Stage generated agent ISO", "Prepare vSphere virtual media", "Power on vSphere machine"},
	},
	{
		name:      "libvirt media lifecycle",
		provider:  "libvirt",
		path:      bootwrightCollectionRoleRoot + "/container_cluster_media_libvirt/tasks/main.yml",
		gates:     []string{"Validate direct libvirt virtual media action"},
		mutations: []string{"Clean stale running virtual media before insert", "Clean stale persistent virtual media before insert", "Insert staged virtual media directly into libvirt domain"},
	},
	{
		name:      "package ownership apply",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/package_apply.yml",
		gates:     []string{"Validate package ownership apply inputs"},
		mutations: []string{"Install owned packages"},
		evidence:  []string{"Write ownership records for owned packages"},
	},
	{
		name:      "package ownership removal",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/package_remove.yml",
		gates:     []string{"Validate package ownership removal inputs"},
		mutations: []string{"Remove ownership-gated packages"},
	},
	{
		name:      "package ownership member removal",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/package_remove_one.yml",
		gates:     []string{"Stat package ownership record"},
		mutations: []string{"Remove package that Bootwright introduced"},
		evidence:  []string{"Remove package ownership record"},
	},
	{
		name:      "recorded managed OS destroy",
		provider:  "baremetal",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/destroy_declared_managed_os.yml",
		gates:     []string{"Refuse unrecorded present managed OS artifacts"},
		mutations: []string{"Destroy exactly recorded declared managed OS artifacts"},
	},
	{
		name:      "provider destroy dispatch",
		provider:  "all",
		path:      "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/destroy_machine.yml",
		gates:     []string{"Validate selected machine component"},
		mutations: []string{"Dispatch selected machine substrate destroy"},
	},
	{
		name:      "libvirt network destroy",
		provider:  "libvirt",
		path:      "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/prepare_destroy_cluster.yml",
		gates:     []string{"Refuse to remove a non-Bootwright libvirt network"},
		mutations: []string{"Authorize cluster libvirt network removal"},
	},
	{
		name:      "managed load balancer detach",
		provider:  "all",
		path:      "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/prepare_destroy_cluster.yml",
		gates:     []string{"Resolve current cluster"},
		mutations: []string{"Detach managed load balancer VIPs"},
	},
	{
		name:     "libvirt network apply",
		provider: "libvirt",
		path:     bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/network.yml",
		gates: []string{
			"Require a conclusive libvirt network probe before apply",
			"Refuse to redefine a foreign libvirt network",
		},
		mutations: []string{"Create per-cluster libvirt state directory", "Define libvirt network", "Activate libvirt network"},
		evidence:  []string{"Record libvirt network ownership"},
	},
	{
		name:     "libvirt domain apply",
		provider: "libvirt",
		path:     bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/machine.yml",
		gates: []string{
			"Require a conclusive libvirt domain probe",
			"Refuse to mutate a non-Bootwright libvirt domain on apply",
			"Enforce libvirt domain apply mode against live state",
		},
		mutations: []string{"Create per-machine libvirt state directories", "Define libvirt domain"},
		evidence:  []string{"Record libvirt domain ownership"},
	},
	{
		name:     "libvirt domain destroy",
		provider: "libvirt",
		path:     bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/destroy.yml",
		gates: []string{
			"Require a conclusive libvirt domain probe before destroy",
			"Refuse to destroy a non-Bootwright libvirt domain",
		},
		mutations: []string{"Stop libvirt domain", "Undefine libvirt domain"},
		evidence:  []string{"Remove libvirt domain ownership record"},
	},
	{
		name:     "libvirt context sweep",
		provider: "libvirt",
		path:     bootwrightCollectionRoleRoot + "/provider_host_libvirt/tasks/destroy_context.yml",
		gates: []string{
			"Require conclusive libvirt block-device probes before context sweep",
			"Require conclusive libvirt ownership probes before context sweep",
			"Require consistent libvirt probe results before context sweep",
		},
		mutations: []string{"Stop current-context libvirt domains", "Undefine current-context libvirt domains"},
		evidence:  []string{"Remove current-context libvirt ownership records"},
	},
	{
		name:     "kubevirt substrate apply",
		provider: "kubevirt",
		path:     bootwrightCollectionRoleRoot + "/machine_substrate_kubevirt/tasks/main.yml",
		gates: []string{
			"Require a conclusive KubeVirt VirtualMachine probe",
			"Require a conclusive KubeVirt root DataVolume probe",
			"Enforce KubeVirt VirtualMachine apply mode",
			"Enforce KubeVirt root DataVolume apply mode",
			"Refuse a reused KubeVirt root claim whose volume mode drifted from its storage profile",
		},
		mutations: []string{"Stop KubeVirt VirtualMachine for authorized rebuild", "Apply KubeVirt root disk DataVolume", "Apply KubeVirt VirtualMachine"},
		evidence:  []string{"Record KubeVirt machine ownership"},
	},
	{
		name:     "kubevirt substrate destroy",
		provider: "kubevirt",
		path:     bootwrightCollectionRoleRoot + "/machine_substrate_kubevirt/tasks/destroy.yml",
		gates: []string{
			"Require a conclusive KubeVirt VirtualMachine probe",
			"Refuse to delete a non-Bootwright KubeVirt VirtualMachine",
			"Require conclusive KubeVirt DataVolume probes",
			"Require conclusive KubeVirt PersistentVolumeClaim probes",
			"Refuse to delete a foreign KubeVirt DataVolume",
			"Refuse to delete a foreign KubeVirt PersistentVolumeClaim",
		},
		mutations: []string{"Delete KubeVirt VirtualMachine", "Delete KubeVirt DataVolumes", "Purge the KubeVirt root disk PersistentVolumeClaim"},
		evidence:  []string{"Remove KubeVirt machine ownership record"},
	},
	{
		name:      "vsphere apply dispatch",
		provider:  "vsphere",
		path:      bootwrightCollectionRoleRoot + "/machine_substrate_vsphere/tasks/main.yml",
		gates:     []string{"Probe vSphere virtual machine", "Gate vSphere virtual machine changes"},
		mutations: []string{"Apply vSphere virtual machine"},
		evidence:  []string{"Record vSphere machine ownership"},
	},
	{
		name:     "vsphere substrate destroy",
		provider: "vsphere",
		path:     bootwrightCollectionRoleRoot + "/machine_substrate_vsphere/tasks/destroy.yml",
		gates: []string{
			"Require exact recorded vSphere virtual media ownership before destroy",
			"Require a conclusive vSphere VM probe before destroy",
			"Refuse to delete a non-Bootwright vSphere VM",
		},
		mutations: []string{"Delete vSphere VM", "Delete recorded vSphere virtual media from the datastore"},
		evidence:  []string{"Remove recorded vSphere virtual media staging paths and records", "Remove vSphere machine ownership record"},
	},
	{
		name:      "vsphere media replacement",
		provider:  "vsphere",
		path:      bootwrightCollectionRoleRoot + "/container_cluster_media_vsphere/tasks/insert.yml",
		gates:     []string{"Require exact vSphere virtual media ownership before replacement"},
		mutations: []string{"Delete superseded vSphere virtual media from the datastore"},
		evidence:  []string{"Remove superseded vSphere virtual media staging paths and record"},
	},
	{
		name:      "vsphere media cleanup",
		provider:  "vsphere",
		path:      bootwrightCollectionRoleRoot + "/container_cluster_media_vsphere/tasks/cleanup.yml",
		gates:     []string{"Require exact vSphere virtual media ownership before cleanup"},
		mutations: []string{"Remove vSphere virtual media drive", "Delete uploaded vSphere virtual media from the datastore"},
		evidence:  []string{"Remove recorded vSphere virtual media staging paths and record"},
	},
	{
		name:      "ownership record write",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/resource.yml",
		gates:     []string{"Refuse to replace contradictory ownership evidence"},
		mutations: []string{"Create ownership resource directory"},
		evidence:  []string{"Write ownership resource record"},
	},
	{
		name:      "ownership record removal",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/remove_resource.yml",
		gates:     []string{"Require exact ownership resource authority before removal"},
		mutations: []string{"Remove ownership resource record"},
	},
	{
		name:      "recorded provider resource destroy",
		provider:  "all",
		path:      bootwrightCollectionRoleRoot + "/ownership_record/tasks/destroy_resource.yml",
		gates:     []string{"Revalidate recorded resource before teardown", "Require live record or positive all-members-absent replay"},
		mutations: []string{"Stop recorded libvirt domain", "Undefine recorded libvirt network", "Remove recorded podman container"},
		evidence:  []string{"Remove destroyed ownership resource record"},
	},
}

var providerReadOnlyTaskActions = map[string]bool{
	"ansible.builtin.assert":                  true,
	"ansible.builtin.debug":                   true,
	"ansible.builtin.fail":                    true,
	"ansible.builtin.find":                    true,
	"ansible.builtin.gather_facts":            true,
	"ansible.builtin.include_vars":            true,
	"ansible.builtin.meta":                    true,
	"ansible.builtin.package_facts":           true,
	"ansible.builtin.pause":                   true,
	"ansible.builtin.service_facts":           true,
	"ansible.builtin.set_fact":                true,
	"ansible.builtin.setup":                   true,
	"ansible.builtin.slurp":                   true,
	"ansible.builtin.stat":                    true,
	"ansible.builtin.wait_for":                true,
	"ansible.builtin.wait_for_connection":     true,
	"community.vmware.vmware_datastore_info":  true,
	"community.vmware.vmware_guest_disk_info": true,
	"community.vmware.vmware_guest_info":      true,
	"kubernetes.core.k8s_info":                true,
}

var providerTaskControlKeys = map[string]bool{
	"args":             true,
	"always":           true,
	"any_errors_fatal": true,
	"become":           true,
	"block":            true,
	"changed_when":     true,
	"check_mode":       true,
	"delay":            true,
	"delegate_to":      true,
	"environment":      true,
	"failed_when":      true,
	"ignore_errors":    true,
	"loop":             true,
	"loop_control":     true,
	"module_defaults":  true,
	"name":             true,
	"no_log":           true,
	"notify":           true,
	"register":         true,
	"rescue":           true,
	"retries":          true,
	"run_once":         true,
	"tags":             true,
	"throttle":         true,
	"until":            true,
	"vars":             true,
	"when":             true,
}

func TestEverySupportedProviderHasAMutationSafetyRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, entry := range roles.Entries() {
		if entry.Status != roles.StatusSupported || entry.Dispatch.SubstrateRole == "none" {
			continue
		}
		provider := entry.Dispatch.SubstrateRole
		seen[provider] = true
		if !providerMutationProviders[provider] {
			t.Errorf("supported substrate provider %q has no mutation-safety registry entry", provider)
		}
	}
	for provider := range providerMutationProviders {
		if !seen[provider] {
			t.Errorf("mutation-safety registry retains unsupported substrate provider %q", provider)
		}
	}
}

func TestEveryProviderStateChangingTaskFileIsRegistered(t *testing.T) {
	seenFiles := map[string]bool{}
	seenTasks := map[providerMutationTask]bool{}
	seenTaskDefinitions := map[providerMutationTask]map[string]any{}
	visit := func(rel string) {
		tasks := flattenAnsibleTasks(readAnsibleTasks(t, rel))
		if rel == "ansible/collections/ansible_collections/bootwright/core/playbooks/task_host_shared_service_operation_finalize.yml" {
			tasks = nil
			for _, play := range readAnsibleTasks(t, rel) {
				raw, ok := play["tasks"].([]any)
				if !ok {
					t.Fatalf("host shared-service finalizer play is missing tasks: %v", play)
				}
				var playTasks []map[string]any
				for _, item := range raw {
					task, ok := item.(map[string]any)
					if !ok {
						t.Fatalf("host shared-service finalizer has malformed task: %v", item)
					}
					playTasks = append(playTasks, task)
				}
				tasks = append(tasks, flattenAnsibleTasks(playTasks)...)
			}
		}
		for _, task := range tasks {
			action, changes := providerTaskStateChangingAction(task)
			if !changes {
				continue
			}
			seenFiles[rel] = true
			if providerMutationTaskFiles[rel] == "" {
				t.Errorf("provider task file %s can change state but has no providerMutationTaskFiles classification", rel)
			}
			ref := providerMutationTask{path: rel, name: strings.TrimSpace(fmt.Sprint(task["name"])), action: action}
			seenTasks[ref] = true
			seenTaskDefinitions[ref] = task
			if _, found := providerMutationTasks[ref]; !found {
				t.Errorf("provider state-capable task is unregistered: path=%q name=%q action=%q", ref.path, ref.name, ref.action)
			}
		}
	}
	for _, role := range ansibleAdapterRoleDirs(t) {
		if !providerMutationAdapterRole(role) {
			continue
		}
		root := filepath.Join(repoRoot(t), filepath.FromSlash(bootwrightCollectionRoleRoot), strings.TrimPrefix(role, "bootwright.core."), "tasks")
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".yml" {
				return nil
			}
			rel, err := filepath.Rel(repoRoot(t), path)
			if err != nil {
				return err
			}
			visit(filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{
		bootwrightCollectionRoleRoot + "/ownership_record/tasks",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra",
	} {
		abs := filepath.Join(repoRoot(t), filepath.FromSlash(root))
		entries, err := os.ReadDir(abs)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".yml" {
				continue
			}
			visit(filepath.ToSlash(filepath.Join(root, entry.Name())))
		}
	}
	visit("ansible/collections/ansible_collections/bootwright/core/playbooks/task_host_shared_service_operation_finalize.yml")
	for path := range providerMutationTaskFiles {
		if !seenFiles[path] {
			t.Errorf("providerMutationTaskFiles entry %s no longer contains a state-capable task", path)
		}
	}
	knownSurfaces := map[string]providerMutationSurface{}
	for _, surface := range providerMutationSurfaces {
		if _, found := knownSurfaces[surface.name]; found {
			t.Errorf("provider mutation safety surface %q is registered more than once", surface.name)
		}
		knownSurfaces[surface.name] = surface
	}
	for ref, registration := range providerMutationTasks {
		class := registration.class
		if class != "probe" && class != "gate" && class != "mutation" && class != "removal" && class != "evidence" && class != "delegated" {
			t.Errorf("provider mutation task %#v has unknown class %q", ref, class)
		}
		surface, found := knownSurfaces[registration.surface]
		if !found {
			t.Errorf("provider mutation task %#v names unregistered safety surface %q", ref, registration.surface)
			continue
		}
		var orderedAnchors []string
		switch class {
		case "probe", "gate":
			orderedAnchors = surface.gates
		case "mutation", "removal", "delegated":
			orderedAnchors = surface.mutations
		case "evidence":
			orderedAnchors = surface.evidence
		}
		if !slices.Contains(orderedAnchors, registration.anchor) {
			t.Errorf("provider mutation task %#v class %q must bind to an ordered %s anchor on safety surface %q; got %q", ref, class, class, registration.surface, registration.anchor)
		}
		if !seenTasks[ref] {
			t.Errorf("provider mutation task registry retains missing task: path=%q name=%q action=%q", ref.path, ref.name, ref.action)
			continue
		}
		if !providerMutationClassMatchesAction(ref.action, seenTaskDefinitions[ref][ref.action], class) {
			t.Errorf("provider mutation task %#v class %q contradicts its action parameters", ref, class)
		}
	}
}

func TestProviderMutationRegistryGatesBeforeSideEffectsAndDeletesEvidenceLast(t *testing.T) {
	for _, surface := range providerMutationSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			tasks := flattenAnsibleTasks(readAnsibleTasks(t, surface.path))
			lastGate := -1
			firstMutation := len(tasks)
			lastMutation := -1
			firstEvidence := len(tasks)
			for _, name := range surface.gates {
				idx := findAnsibleTask(t, tasks, name)
				if idx > lastGate {
					lastGate = idx
				}
			}
			for _, name := range surface.mutations {
				idx := findAnsibleTask(t, tasks, name)
				if idx < firstMutation {
					firstMutation = idx
				}
				if idx > lastMutation {
					lastMutation = idx
				}
			}
			for _, name := range surface.evidence {
				idx := findAnsibleTask(t, tasks, name)
				if idx < firstEvidence {
					firstEvidence = idx
				}
			}
			if lastGate < 0 || firstMutation == len(tasks) || lastGate >= firstMutation {
				t.Fatalf("%s must place every registered identity gate before the first side effect: gate=%d mutation=%d", surface.path, lastGate, firstMutation)
			}
			if len(surface.evidence) > 0 && (lastMutation < 0 || firstEvidence == len(tasks) || lastMutation >= firstEvidence) {
				t.Fatalf("%s must retain evidence until every registered mutation/removal succeeds: mutation=%d evidence=%d", surface.path, lastMutation, firstEvidence)
			}
		})
	}
}

func providerMutationAdapterRole(role string) bool {
	name := strings.TrimPrefix(role, "bootwright.core.")
	for _, prefix := range []string{"machine_substrate_", "provider_host_", "provider_service_bmc_", "container_cluster_boot_", "container_cluster_media_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func providerTaskStateChangingAction(task map[string]any) (string, bool) {
	for key := range task {
		if providerTaskControlKeys[key] || providerReadOnlyTaskActions[key] {
			continue
		}
		return key, true
	}
	return "", false
}

func providerMutationClassMatchesAction(action string, raw any, class string) bool {
	sideEffect := class == "mutation" || class == "removal" || class == "evidence"
	params, _ := raw.(map[string]any)
	switch action {
	case "ansible.builtin.uri":
		method := "GET"
		if rawMethod, found := params["method"]; found {
			method = strings.ToUpper(strings.TrimSpace(fmt.Sprint(rawMethod)))
		}
		if method == "GET" || method == "HEAD" || method == "OPTIONS" {
			return class == "probe" || class == "gate"
		}
		if method == "DELETE" {
			return class == "removal"
		}
		return sideEffect
	case "ansible.builtin.file":
		if strings.TrimSpace(fmt.Sprint(params["state"])) == "absent" {
			return class == "removal" || class == "evidence"
		}
		return class == "mutation" || class == "evidence"
	case "ansible.builtin.copy", "ansible.builtin.package", "ansible.builtin.pip", "ansible.builtin.service", "ansible.builtin.systemd", "ansible.builtin.systemd_service", "ansible.builtin.template", "ansible.posix.firewalld", "ansible.posix.mount", "community.general.sefcontext", "community.libvirt.virt", "community.libvirt.virt_net", "community.libvirt.virt_pool", "community.vmware.vmware_guest", "community.vmware.vmware_guest_boot_manager", "community.vmware.vmware_guest_powerstate", "community.vmware.vsphere_copy", "community.vmware.vsphere_file", "containers.podman.podman_container", "kubernetes.core.k8s":
		return sideEffect
	}
	return true
}
