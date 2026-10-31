package model

type TrafficLimit struct {
	Method   string `json:"method"`
	Bytes    *int64 `json:"bytes"`
	Unit     string `json:"unit"`
	ResetDay int    `json:"resetDay"`
}

type GeoIPConfig struct {
	Enabled   bool   `json:"enabled"`
	Provider  string `json:"provider"`
	UserAgent string `json:"userAgent"`
}

type AgentTask struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Target   string `json:"target"`
	Interval int    `json:"interval"`
	Timeout  int    `json:"timeout"`
}

type AgentConfig struct {
	ArchiveSeconds int          `json:"archiveSeconds"`
	Version        string       `json:"version"`
	Refresh        int          `json:"refresh"`
	Traffic        TrafficLimit `json:"traffic"`
	Timezone       string       `json:"timezone"`
	GeoIP          GeoIPConfig  `json:"geoip"`
	Tasks          []AgentTask  `json:"tasks"`
}

type AgentInfo struct {
	IP       string `json:"ip"`
	IPv6     string `json:"ipv6,omitempty"`
	Version  string `json:"version"`
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
	Arch     string `json:"arch"`
	CPUModel string `json:"cpuModel"`
	Cores    int    `json:"cores"`
	Memory   int64  `json:"memory"`
	Storage  int64  `json:"storage"`
	Region   string `json:"region"`
	City     string `json:"city"`
}

type Report struct {
	Time      int64   `json:"time"`
	BootID    string  `json:"bootId"`
	Sequence  int64   `json:"sequence"`
	Uptime    float64 `json:"uptime"`
	CPU       float64 `json:"cpu"`
	Mem       float64 `json:"mem"`
	Disk      float64 `json:"disk"`
	Load      float64 `json:"load"`
	Up        float64 `json:"up"`
	Down      float64 `json:"down"`
	TotalUp   int64   `json:"totalUp"`
	TotalDown int64   `json:"totalDown"`
}

// offset milliseconds, sequence, uptime, CPU, MEM, DISK, load, up, down, totalUp, totalDown.
type PackedSample [11]float64
type Archive struct {
	BootID  string          `json:"bootId"`
	Start   int64           `json:"start"`
	Samples []PackedSample  `json:"samples"`
	Latency []LatencyResult `json:"latency"`
}
type ClientMessage struct {
	Type    string          `json:"type"`
	Report  *Report         `json:"report,omitempty"`
	Results []LatencyResult `json:"results,omitempty"`
	Archive *Archive        `json:"archive,omitempty"`
}
type ServerMessage struct {
	Type     string      `json:"type"`
	Config   AgentConfig `json:"config"`
	BootID   string      `json:"bootId"`
	Start    int64       `json:"start"`
	Sequence int64       `json:"sequence"`
	Message  string      `json:"message"`
}

type LatencyResult struct {
	TaskID string   `json:"taskId"`
	RunID  string   `json:"runId"`
	Status string   `json:"status"`
	Value  *float64 `json:"value"`
	Time   int64    `json:"time"`
}
