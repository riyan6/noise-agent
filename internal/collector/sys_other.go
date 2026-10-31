//go:build !linux

package collector

import (
	"runtime"
	"time"
)

var processStart = time.Now()

type hardwareInfo struct {
	OS       string
	Kernel   string
	CPUModel string
	Memory   int64
	Storage  int64
}

func readHardwareInfo() hardwareInfo {
	return hardwareInfo{
		OS:       runtime.GOOS,
		Kernel:   runtime.Version(),
		CPUModel: runtime.GOARCH + " CPU",
		Memory:   4 * 1024 * 1024 * 1024,
		Storage:  80 * 1024 * 1024 * 1024,
	}
}

func readCPUTimes() (cpuTimes, error) {
	return cpuTimes{idle: 85, total: 100}, nil
}

func readMemoryPercent() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return 25.0
}

func readDiskPercent() float64 {
	return 30.0
}

func readLoadAvg() float64 {
	return 0.25
}

func readOSUptime() float64 {
	return time.Since(processStart).Seconds()
}

func readNetCounters() (netCounters, error) {
	return netCounters{}, nil
}
