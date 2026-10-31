//go:build linux

package collector

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type hardwareInfo struct {
	OS       string
	Kernel   string
	CPUModel string
	Memory   int64
	Storage  int64
}

func readHardwareInfo() hardwareInfo {
	var totalMem, _ = readMemStats()
	var totalDisk, _ = readDiskStats()
	return hardwareInfo{
		OS:       readOSRelease(),
		Kernel:   readKernelRelease(),
		CPUModel: readCPUModel(),
		Memory:   totalMem,
		Storage:  totalDisk,
	}
}

func readOSRelease() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	defer f.Close()
	var pretty, name, version string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "PRETTY_NAME":
			pretty = v
		case "NAME":
			name = v
		case "VERSION_ID":
			version = v
		}
	}
	if pretty != "" {
		return pretty
	}
	if name != "" && version != "" {
		return name + " " + version
	}
	if name != "" {
		return name
	}
	return "Linux"
}

func readKernelRelease() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readCPUModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "Linux CPU"
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var fallback string
	for scanner.Scan() {
		line := scanner.Text()
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if k == "model name" || k == "cpu model" {
			return v
		}
		if (k == "hardware" || k == "processor" || k == "cpu") && fallback == "" {
			fallback = v
		}
	}
	if fallback != "" {
		return fallback
	}
	return "Linux CPU"
}

func readCPUTimes() (cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			break
		}
		var total, idle uint64
		for i, field := range fields[1:] {
			val, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				continue
			}
			total += val
			// fields[1:] indices: 0=user, 1=nice, 2=system, 3=idle, 4=iowait
			if i == 3 || i == 4 {
				idle += val
			}
		}
		return cpuTimes{idle: idle, total: total}, nil
	}
	return cpuTimes{}, os.ErrNotExist
}

func readMemStats() (totalBytes int64, usedPercent float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var totalKB, availKB, freeKB, buffersKB, cachedKB, sreclaimableKB uint64
	var hasAvail bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			totalKB = val
		case "MemAvailable":
			availKB = val
			hasAvail = true
		case "MemFree":
			freeKB = val
		case "Buffers":
			buffersKB = val
		case "Cached":
			cachedKB = val
		case "SReclaimable":
			sreclaimableKB = val
		}
	}
	if totalKB == 0 {
		return 0, 0
	}
	if !hasAvail {
		availKB = freeKB + buffersKB + cachedKB + sreclaimableKB
	}
	if availKB > totalKB {
		availKB = totalKB
	}
	usedKB := totalKB - availKB
	return int64(totalKB * 1024), 100.0 * float64(usedKB) / float64(totalKB)
}

func readMemoryPercent() float64 {
	_, pct := readMemStats()
	return pct
}

func readDiskStats() (totalBytes int64, usedPercent float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0
	}
	bsize := uint64(stat.Bsize)
	total := stat.Blocks * bsize
	if total == 0 {
		return 0, 0
	}
	free := stat.Bfree * bsize
	if free > total {
		free = total
	}
	used := total - free
	return int64(total), 100.0 * float64(used) / float64(total)
}

func readDiskPercent() float64 {
	_, pct := readDiskStats()
	return pct
}

func readLoadAvg() float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return val
}

func readOSUptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return val
}

func readNetCounters() (netCounters, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return netCounters{}, err
	}
	defer f.Close()
	var total netCounters
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		iface, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if isVirtualInterface(iface) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 == nil {
			total.rx += rx
		}
		if err2 == nil {
			total.tx += tx
		}
	}
	return total, nil
}
