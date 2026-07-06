package cephstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Read names are the stable filenames the discover_storage_state playbook writes
// (one per `ceph ... --format json` read). They are the keys into Discovery.Reads
// and must match the playbook's read list.
const (
	ReadStatus        = "status"
	ReadHealth        = "health"
	ReadOSDStat       = "osd_stat"
	ReadVersions      = "versions"
	ReadOrchLS        = "orch_ls"
	ReadOrchPS        = "orch_ps"
	ReadOrchHostLS    = "orch_host_ls"
	ReadOrchDeviceLS  = "orch_device_ls"
	ReadOSDTree       = "osd_tree"
	ReadOSDDF         = "osd_df"
	ReadOSDMetadata   = "osd_metadata"
	ReadCrushDump     = "crush_dump"
	ReadCrushRuleDump = "crush_rule_dump"
	ReadPoolLSDetail  = "pool_ls_detail"
	ReadECProfileLS   = "ec_profile_ls"
	ReadConfigDump    = "config_dump"
	ReadMgrModuleLS   = "mgr_module_ls"
	ReadFSDump        = "fs_dump"
	ReadNFSClusterLS  = "nfs_cluster_ls"
	ReadMonDump       = "mon_dump"

	markerFileName = "discovery.json"
)

// ErrReadAbsent means the requested read was not collected for this cluster —
// the command failed on the seed (e.g. the feature is not deployed) or the
// cluster was not reachable. Decoders return it wrapped so callers can treat an
// absent facet as "unknown, do not diff" rather than "empty on the cluster".
var ErrReadAbsent = errors.New("ceph discovery read not present")

// Discovery is one managed Ceph cluster's raw live observation: the JSON blob of
// each successful read, plus the marker the playbook wrote. Probed is false when
// the seed was reached but no read succeeded (e.g. cluster never bootstrapped);
// a cluster the playbook could not reach at all has no Discovery.
type Discovery struct {
	Cluster string                     `json:"cluster"`
	Probed  bool                       `json:"probed"`
	Reads   map[string]json.RawMessage `json:"-"`
}

// marker mirrors the discovery.json the playbook writes.
type marker struct {
	Cluster string   `json:"cluster"`
	Probed  bool     `json:"probed"`
	Reads   []string `json:"reads"`
}

// Load reads a discovery directory produced by the playbook (one subdirectory
// per cluster) and returns each cluster's Discovery keyed by cluster name. A
// subdirectory with no marker is skipped. A malformed individual read is kept as
// raw bytes (decoding fails later, per facet) rather than failing the whole
// load, so one bad blob never blinds the entire diff.
func Load(dir string) (map[string]Discovery, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Discovery{}, nil
		}
		return nil, fmt.Errorf("read discovery directory %s: %w", dir, err)
	}
	out := map[string]Discovery{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		disc, ok, err := loadCluster(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if ok && disc.Cluster != "" {
			out[disc.Cluster] = disc
		}
	}
	return out, nil
}

func loadCluster(clusterDir string) (Discovery, bool, error) {
	markerData, err := os.ReadFile(filepath.Join(clusterDir, markerFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return Discovery{}, false, nil
		}
		return Discovery{}, false, fmt.Errorf("read discovery marker in %s: %w", clusterDir, err)
	}
	var m marker
	if err := json.Unmarshal(markerData, &m); err != nil {
		return Discovery{}, false, fmt.Errorf("decode discovery marker in %s: %w", clusterDir, err)
	}
	files, err := os.ReadDir(clusterDir)
	if err != nil {
		return Discovery{}, false, fmt.Errorf("read discovery directory %s: %w", clusterDir, err)
	}
	reads := map[string]json.RawMessage{}
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || name == markerFileName || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(clusterDir, name))
		if err != nil {
			return Discovery{}, false, fmt.Errorf("read discovery file %s: %w", name, err)
		}
		reads[strings.TrimSuffix(name, ".json")] = json.RawMessage(data)
	}
	return Discovery{Cluster: m.Cluster, Probed: m.Probed, Reads: reads}, true, nil
}

// Has reports whether a read was collected for this cluster.
func (d Discovery) Has(read string) bool {
	_, ok := d.Reads[read]
	return ok
}

// decode unmarshals a named read into value, returning ErrReadAbsent when the
// read was not collected.
func (d Discovery) decode(read string, value any) error {
	raw, ok := d.Reads[read]
	if !ok {
		return fmt.Errorf("%s: %w", read, ErrReadAbsent)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("decode ceph %s read: %w", read, err)
	}
	return nil
}

// === Typed facets (field names mirror Ceph's JSON) ===

// Service is one cephadm service as reported by `ceph orch ls --format json`.
type Service struct {
	ServiceType string           `json:"service_type"`
	ServiceName string           `json:"service_name"`
	ServiceID   string           `json:"service_id"`
	Placement   ServicePlacement `json:"placement"`
	Status      ServiceStatus    `json:"status"`
	Spec        json.RawMessage  `json:"spec"`
}

type ServicePlacement struct {
	Hosts        []string `json:"hosts"`
	Count        int      `json:"count"`
	CountPerHost int      `json:"count_per_host"`
	Label        string   `json:"label"`
	HostPattern  string   `json:"host_pattern"`
}

type ServiceStatus struct {
	Running int `json:"running"`
	Size    int `json:"size"`
}

// Services decodes `ceph orch ls`.
func (d Discovery) Services() ([]Service, error) {
	var out []Service
	if err := d.decode(ReadOrchLS, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceName < out[j].ServiceName })
	return out, nil
}

// Daemon is one running daemon from `ceph orch ps --format json`. It resolves a
// service's real placement: the set of hosts actually running its daemons,
// which `ceph orch ls` does not report directly.
type Daemon struct {
	DaemonType  string `json:"daemon_type"`
	DaemonID    string `json:"daemon_id"`
	ServiceName string `json:"service_name"`
	Hostname    string `json:"hostname"`
	Status      string `json:"status_desc"`
}

// Daemons decodes `ceph orch ps`.
func (d Discovery) Daemons() ([]Daemon, error) {
	var out []Daemon
	if err := d.decode(ReadOrchPS, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ServiceHosts returns, per service name, the sorted set of hosts running its
// daemons — the real placement to diff against a desired service's resolved
// placement. Returns an empty map (not an error) when orch ps was not collected.
func (d Discovery) ServiceHosts() map[string][]string {
	daemons, err := d.Daemons()
	if err != nil {
		return map[string][]string{}
	}
	sets := map[string]map[string]struct{}{}
	for _, daemon := range daemons {
		if daemon.ServiceName == "" || daemon.Hostname == "" {
			continue
		}
		if sets[daemon.ServiceName] == nil {
			sets[daemon.ServiceName] = map[string]struct{}{}
		}
		sets[daemon.ServiceName][daemon.Hostname] = struct{}{}
	}
	out := map[string][]string{}
	for service, hosts := range sets {
		list := make([]string, 0, len(hosts))
		for host := range hosts {
			list = append(list, host)
		}
		sort.Strings(list)
		out[service] = list
	}
	return out
}

// Host is one registered cephadm host from `ceph orch host ls --format json`.
type Host struct {
	Hostname string   `json:"hostname"`
	Addr     string   `json:"addr"`
	Labels   []string `json:"labels"`
	Status   string   `json:"status"`
}

// Hosts decodes `ceph orch host ls`.
func (d Discovery) Hosts() ([]Host, error) {
	var out []Host
	if err := d.decode(ReadOrchHostLS, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, nil
}

// HostDevices is one host's inventory from `ceph orch device ls --format json`.
type HostDevices struct {
	Host    string   `json:"addr"`
	Name    string   `json:"name"`
	Devices []Device `json:"devices"`
}

type Device struct {
	Path      string       `json:"path"`
	Available bool         `json:"available"`
	SysAPI    DeviceSysAPI `json:"sys_api"`
}

type DeviceSysAPI struct {
	Rotational string `json:"rotational"`
	Size       int64  `json:"size"`
	Model      string `json:"model"`
	Vendor     string `json:"vendor"`
}

// Devices decodes `ceph orch device ls`.
func (d Discovery) Devices() ([]HostDevices, error) {
	var out []HostDevices
	if err := d.decode(ReadOrchDeviceLS, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// OSDTreeNode is one node of `ceph osd tree --format json` (root/host/osd/...).
type OSDTreeNode struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Children    []int   `json:"children"`
	DeviceClass string  `json:"device_class"`
	CrushWeight float64 `json:"crush_weight"`
	Status      string  `json:"status"`
}

// OSDTree decodes `ceph osd tree` into its node list.
func (d Discovery) OSDTree() ([]OSDTreeNode, error) {
	var wrap struct {
		Nodes []OSDTreeNode `json:"nodes"`
		Stray []OSDTreeNode `json:"stray"`
	}
	if err := d.decode(ReadOSDTree, &wrap); err != nil {
		return nil, err
	}
	return wrap.Nodes, nil
}

// OSDMeta is one OSD's metadata from `ceph osd metadata --format json`: the
// daemon id, the host it runs on, and the comma-separated kernel device names
// backing it (the `devices` field, e.g. "sdb" or "sdb,sdc" for a multi-device
// OSD). It is the only read that maps an OSD to the physical devices it consumed.
type OSDMeta struct {
	ID       int    `json:"id"`
	Hostname string `json:"hostname"`
	Devices  string `json:"devices"`
}

// OSDMetadata decodes `ceph osd metadata`.
func (d Discovery) OSDMetadata() ([]OSDMeta, error) {
	var out []OSDMeta
	if err := d.decode(ReadOSDMetadata, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// OSDDevicesByHost returns, keyed by ceph hostname, the sorted deduplicated set
// of kernel device basenames backing that host's OSDs (from `ceph osd
// metadata`). It is the ground truth for which physical devices a host's OSDs
// actually consumed — precisely what a filter/all drivegroup selection leaves
// implicit in desired state. It returns ErrReadAbsent (wrapped) when the read
// was not collected, so an absent read reads as "unknown, do not diff" rather
// than "this host has no OSDs".
func (d Discovery) OSDDevicesByHost() (map[string][]string, error) {
	metas, err := d.OSDMetadata()
	if err != nil {
		return nil, err
	}
	byHost := map[string]map[string]bool{}
	for _, meta := range metas {
		if meta.Hostname == "" {
			continue
		}
		set := byHost[meta.Hostname]
		if set == nil {
			set = map[string]bool{}
			byHost[meta.Hostname] = set
		}
		for _, dev := range strings.Split(meta.Devices, ",") {
			if dev = strings.TrimSpace(dev); dev != "" {
				set[dev] = true
			}
		}
	}
	out := make(map[string][]string, len(byHost))
	for host, set := range byHost {
		devices := make([]string, 0, len(set))
		for dev := range set {
			devices = append(devices, dev)
		}
		sort.Strings(devices)
		out[host] = devices
	}
	return out, nil
}

// Pool data-protection types as Ceph reports them in the numeric `type` field.
const (
	PoolTypeReplicated = 1
	PoolTypeErasure    = 3
)

// Pool is one pool from `ceph osd pool ls detail --format json`.
type Pool struct {
	ID                  int                        `json:"pool"`
	Name                string                     `json:"pool_name"`
	Type                int                        `json:"type"`
	Size                int                        `json:"size"`
	MinSize             int                        `json:"min_size"`
	CrushRule           int                        `json:"crush_rule"`
	ECProfile           string                     `json:"erasure_code_profile"`
	PGNum               int                        `json:"pg_num"`
	ApplicationMetadata map[string]json.RawMessage `json:"application_metadata"`
	Options             map[string]json.RawMessage `json:"options"`
}

// Replicated reports whether the pool uses replication (vs erasure coding).
func (p Pool) Replicated() bool { return p.Type == PoolTypeReplicated }

// Applications returns the pool's enabled application names (rbd/cephfs/rgw).
func (p Pool) Applications() []string {
	names := make([]string, 0, len(p.ApplicationMetadata))
	for name := range p.ApplicationMetadata {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Pools decodes `ceph osd pool ls detail`.
func (d Discovery) Pools() ([]Pool, error) {
	var out []Pool
	if err := d.decode(ReadPoolLSDetail, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CrushRule is one rule from `ceph osd crush rule dump --format json`.
type CrushRule struct {
	ID    int             `json:"rule_id"`
	Name  string          `json:"rule_name"`
	Type  int             `json:"type"`
	Steps []CrushRuleStep `json:"steps"`
}

type CrushRuleStep struct {
	Op       string `json:"op"`
	Type     string `json:"type"`
	Item     int    `json:"item"`
	ItemName string `json:"item_name"`
	Num      int    `json:"num"`
}

// FailureDomain returns the CRUSH bucket type this rule chooses across (the
// chooseleaf/choose step's type, e.g. host or rack), or "" if none is present.
func (r CrushRule) FailureDomain() string {
	for _, step := range r.Steps {
		if strings.HasPrefix(step.Op, "chooseleaf") || strings.HasPrefix(step.Op, "choose") {
			if step.Type != "" {
				return step.Type
			}
		}
	}
	return ""
}

// CrushRules decodes `ceph osd crush rule dump`.
func (d Discovery) CrushRules() ([]CrushRule, error) {
	var out []CrushRule
	if err := d.decode(ReadCrushRuleDump, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CrushRuleByID resolves a rule id (as pools reference it) to its name, "" if
// unknown or if the crush rule dump was not collected.
func (d Discovery) CrushRuleByID(id int) string {
	rules, err := d.CrushRules()
	if err != nil {
		return ""
	}
	for _, rule := range rules {
		if rule.ID == id {
			return rule.Name
		}
	}
	return ""
}

// ConfigOption is one row of `ceph config dump --format json`.
type ConfigOption struct {
	Section string `json:"section"`
	Name    string `json:"name"`
	Value   string `json:"value"`
}

// Config decodes `ceph config dump`.
func (d Discovery) Config() ([]ConfigOption, error) {
	var out []ConfigOption
	if err := d.decode(ReadConfigDump, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Section == out[j].Section {
			return out[i].Name < out[j].Name
		}
		return out[i].Section < out[j].Section
	})
	return out, nil
}

// MgrModules is `ceph mgr module ls --format json`.
type MgrModules struct {
	EnabledModules  []string `json:"enabled_modules"`
	AlwaysOnModules []string `json:"always_on_modules"`
}

// MgrModules decodes `ceph mgr module ls`.
func (d Discovery) MgrModules() (MgrModules, error) {
	var out MgrModules
	if err := d.decode(ReadMgrModuleLS, &out); err != nil {
		return MgrModules{}, err
	}
	return out, nil
}

// Health is the cluster health from `ceph health detail --format json`.
type Health struct {
	Status string                     `json:"status"`
	Checks map[string]json.RawMessage `json:"checks"`
}

// Health decodes `ceph health detail`.
func (d Discovery) Health() (Health, error) {
	var out Health
	if err := d.decode(ReadHealth, &out); err != nil {
		return Health{}, err
	}
	return out, nil
}

// OSDStat is `ceph osd stat --format json`.
type OSDStat struct {
	NumOSDs   int `json:"num_osds"`
	NumUpOSDs int `json:"num_up_osds"`
	NumInOSDs int `json:"num_in_osds"`
}

// OSDStat decodes `ceph osd stat`.
func (d Discovery) OSDStat() (OSDStat, error) {
	var out OSDStat
	if err := d.decode(ReadOSDStat, &out); err != nil {
		return OSDStat{}, err
	}
	return out, nil
}
