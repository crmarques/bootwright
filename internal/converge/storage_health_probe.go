package converge

// storageHostsGroup is the Ansible inventory group of every managed storage node
// (mirrors render/inventory.GroupStorageHosts, inlined because the import matrix
// keeps internal/converge off internal/render).
const storageHostsGroup = "bootwright_storage_hosts"
