package collector

import (
	"context"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"noise-agent/internal/geoip"
	"noise-agent/internal/model"
)

const Version = "0.2.0"
const maxSafeCounter = 9007199254740991 // 2^53 - 1

type cpuTimes struct {
	idle  uint64
	total uint64
}

type netCounters struct {
	rx uint64
	tx uint64
}

type Collector struct {
	mu       sync.Mutex
	lastCPU  cpuTimes
	hasCPU   bool
	lastNet  netCounters
	lastTime time.Time
	hasNet   bool
}

func New() *Collector {
	c := &Collector{}
	if cpu, err := readCPUTimes(); err == nil {
		c.lastCPU = cpu
		c.hasCPU = true
	}
	if netStat, err := readNetCounters(); err == nil {
		c.lastNet = netStat
		c.lastTime = time.Now()
		c.hasNet = true
	}
	return c
}

func (c *Collector) CollectInfo(ctx context.Context, geoCfg model.GeoIPConfig) model.AgentInfo {
	hw := readHardwareInfo()
	placement := geoip.Resolve(ctx, geoCfg)

	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	mem := hw.Memory
	if mem < 1 {
		mem = 1024 * 1024 * 1024
	}
	storage := hw.Storage
	if storage < 0 {
		storage = 0
	}

	return model.AgentInfo{
		IP:       sanitizeText(placement.IP, 256),
		IPv6:     sanitizeText(placement.IPv6, 256),
		Version:  Version,
		OS:       sanitizeText(hw.OS, 256),
		Kernel:   sanitizeText(hw.Kernel, 256),
		Arch:     sanitizeText(normalizeArch(runtime.GOARCH), 256),
		CPUModel: sanitizeText(hw.CPUModel, 256),
		Cores:    cores,
		Memory:   mem,
		Storage:  storage,
		Region:   sanitizeText(placement.Region, 256),
		City:     sanitizeText(placement.City, 256),
	}
}

func (c *Collector) Sample(bootID string, sequence int64) model.Report {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var cpuPct float64
	if curCPU, err := readCPUTimes(); err == nil {
		if c.hasCPU && curCPU.total > c.lastCPU.total {
			totalDelta := float64(curCPU.total - c.lastCPU.total)
			idleDelta := float64(curCPU.idle - c.lastCPU.idle)
			if totalDelta > 0 && totalDelta >= idleDelta {
				cpuPct = clampPercent(100.0 * (totalDelta - idleDelta) / totalDelta)
			}
		}
		c.lastCPU = curCPU
		c.hasCPU = true
	}

	var upRate, downRate float64
	var totalUp, totalDown int64
	if curNet, err := readNetCounters(); err == nil {
		if c.hasNet {
			elapsed := now.Sub(c.lastTime).Seconds()
			if elapsed > 0 {
				if curNet.tx >= c.lastNet.tx {
					upRate = sanitizeFloat(float64(curNet.tx-c.lastNet.tx) / elapsed)
				}
				if curNet.rx >= c.lastNet.rx {
					downRate = sanitizeFloat(float64(curNet.rx-c.lastNet.rx) / elapsed)
				}
			}
		}
		c.lastNet = curNet
		c.lastTime = now
		c.hasNet = true
		totalUp = clampCounter(curNet.tx)
		totalDown = clampCounter(curNet.rx)
	}

	memPct := clampPercent(readMemoryPercent())
	diskPct := clampPercent(readDiskPercent())
	loadAvg := sanitizeFloat(readLoadAvg())
	uptimeSec := sanitizeFloat(readOSUptime())

	return model.Report{
		BootID:    bootID,
		Sequence:  sequence,
		Uptime:    uptimeSec,
		CPU:       cpuPct,
		Mem:       memPct,
		Disk:      diskPct,
		Load:      loadAvg,
		Up:        upRate,
		Down:      downRate,
		TotalUp:   totalUp,
		TotalDown: totalDown,
	}
}

func normalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i686"
	default:
		return goarch
	}
}

func clampPercent(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return math.Round(v*100) / 100
}

func sanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	if v > 1e14 {
		return 1e14
	}
	return math.Round(v*100) / 100
}

func clampCounter(v uint64) int64 {
	if v > uint64(maxSafeCounter) {
		return maxSafeCounter
	}
	return int64(v)
}

func sanitizeText(s string, maxBytes int) string {
	s = strings.NewReplacer("\x00", "", "\r", " ", "\n", " ").Replace(strings.TrimSpace(s))
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	for len(s) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(s)
		if size <= 0 {
			s = s[:maxBytes]
			break
		}
		s = s[:len(s)-size]
	}
	return strings.TrimSpace(s)
}

func isVirtualInterface(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || name == "lo" {
		return true
	}
	for _, prefix := range []string{
		"docker", "veth", "br-", "virbr", "tun", "tap", "wg",
		"flannel", "cni", "cali", "kube", "tailscale", "utun", "sit", "ip6tnl",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
