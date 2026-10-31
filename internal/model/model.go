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
	Version  string       `json:"version"`
	Refresh  int          `json:"refresh"`
	Traffic  TrafficLimit `json:"traffic"`
	Timezone string       `json:"timezone"`
	GeoIP    GeoIPConfig  `json:"geoip"`
	Tasks    []AgentTask  `json:"tasks"`
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

type ReportAck struct {
	ReceivedAt    int64  `json:"receivedAt"`
	ConfigVersion string `json:"configVersion"`
}

type LatencyResult struct {
	TaskID string   `json:"taskId"`
	RunID  string   `json:"runId"`
	Status string   `json:"status"`
	Value  *float64 `json:"value"`
	Time   int64    `json:"time"`
}
