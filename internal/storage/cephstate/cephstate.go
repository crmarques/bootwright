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

var ErrReadAbsent = errors.New("ceph discovery read not present")

type Discovery struct {
	Cluster string                     `json:"cluster"`
	Probed  bool                       `json:"probed"`
	Reads   map[string]json.RawMessage `json:"-"`
}

type marker struct {
	Cluster string   `json:"cluster"`
	Probed  bool     `json:"probed"`
	Reads   []string `json:"reads"`
}

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

func (d Discovery) Has(read string) bool {
	_, ok := d.Reads[read]
	return ok
}

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

func (d Discovery) Services() ([]Service, error) {
	var out []Service
	if err := d.decode(ReadOrchLS, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceName < out[j].ServiceName })
	return out, nil
}

type Daemon struct {
	DaemonType  string `json:"daemon_type"`
	DaemonID    string `json:"daemon_id"`
	ServiceName string `json:"service_name"`
	Hostname    string `json:"hostname"`
	Status      string `json:"status_desc"`
}

func (d Discovery) Daemons() ([]Daemon, error) {
	var out []Daemon
	if err := d.decode(ReadOrchPS, &out); err != nil {
		return nil, err
	}
	return out, nil
}

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

type Host struct {
	Hostname string   `json:"hostname"`
	Addr     string   `json:"addr"`
	Labels   []string `json:"labels"`
	Status   string   `json:"status"`
}

func (d Discovery) Hosts() ([]Host, error) {
	var out []Host
	if err := d.decode(ReadOrchHostLS, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, nil
}

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

func (d Discovery) Devices() ([]HostDevices, error) {
	var out []HostDevices
	if err := d.decode(ReadOrchDeviceLS, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type OSDTreeNode struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Children    []int   `json:"children"`
	DeviceClass string  `json:"device_class"`
	CrushWeight float64 `json:"crush_weight"`
	Status      string  `json:"status"`
}

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

type OSDMeta struct {
	ID       int    `json:"id"`
	Hostname string `json:"hostname"`
	Devices  string `json:"devices"`
}

func (d Discovery) OSDMetadata() ([]OSDMeta, error) {
	var out []OSDMeta
	if err := d.decode(ReadOSDMetadata, &out); err != nil {
		return nil, err
	}
	return out, nil
}

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

const (
	PoolTypeReplicated = 1
	PoolTypeErasure    = 3
)

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

func (p Pool) Replicated() bool { return p.Type == PoolTypeReplicated }

func (p Pool) Applications() []string {
	names := make([]string, 0, len(p.ApplicationMetadata))
	for name := range p.ApplicationMetadata {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (d Discovery) Pools() ([]Pool, error) {
	var out []Pool
	if err := d.decode(ReadPoolLSDetail, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

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

func (d Discovery) CrushRules() ([]CrushRule, error) {
	var out []CrushRule
	if err := d.decode(ReadCrushRuleDump, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

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

type ConfigOption struct {
	Section string `json:"section"`
	Name    string `json:"name"`
	Value   string `json:"value"`
}

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

type MgrModules struct {
	EnabledModules  []string `json:"enabled_modules"`
	AlwaysOnModules []string `json:"always_on_modules"`
}

func (d Discovery) MgrModules() (MgrModules, error) {
	var out MgrModules
	if err := d.decode(ReadMgrModuleLS, &out); err != nil {
		return MgrModules{}, err
	}
	return out, nil
}

type Health struct {
	Status string                     `json:"status"`
	Checks map[string]json.RawMessage `json:"checks"`
}

func (d Discovery) Health() (Health, error) {
	var out Health
	if err := d.decode(ReadHealth, &out); err != nil {
		return Health{}, err
	}
	return out, nil
}

type OSDStat struct {
	NumOSDs   int `json:"num_osds"`
	NumUpOSDs int `json:"num_up_osds"`
	NumInOSDs int `json:"num_in_osds"`
}

func (d Discovery) OSDStat() (OSDStat, error) {
	var out OSDStat
	if err := d.decode(ReadOSDStat, &out); err != nil {
		return OSDStat{}, err
	}
	return out, nil
}
