package system

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"domux/internal/core"
)

type cpuSample struct {
	idle  uint64
	total uint64
}

func Snapshot(ctx context.Context) (core.SystemResources, error) {
	cpu, cpuErr := cpuPercent(ctx)
	memory, memErr := memoryPercent()
	disk, diskErr := diskPercent("/")
	if cpuErr != nil {
		return core.SystemResources{}, cpuErr
	}
	if memErr != nil {
		return core.SystemResources{}, memErr
	}
	if diskErr != nil {
		return core.SystemResources{}, diskErr
	}
	return core.SystemResources{CPUPercent: cpu, MemoryPercent: memory, DiskPercent: disk, CheckedAt: time.Now()}, nil
}

func cpuPercent(ctx context.Context) (float64, error) {
	first, err := readCPUSample()
	if err != nil {
		return 0, err
	}
	timer := time.NewTimer(120 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
	}
	second, err := readCPUSample()
	if err != nil {
		return 0, err
	}
	total := second.total - first.total
	if total == 0 {
		return 0, nil
	}
	idle := second.idle - first.idle
	return clampPercent(100 * float64(total-idle) / float64(total)), nil
}

func readCPUSample() (cpuSample, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return cpuSample{}, fmt.Errorf("read /proc/stat: no cpu line")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}, fmt.Errorf("read /proc/stat: invalid cpu line")
	}
	var values []uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuSample{}, err
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuSample{idle: idle, total: total}, nil
}

func memoryPercent() (float64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var total, available uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = value
		case "MemAvailable":
			available = value
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, fmt.Errorf("read /proc/meminfo: MemTotal missing")
	}
	return clampPercent(100 * float64(total-available) / float64(total)), nil
}

func diskPercent(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	if total == 0 {
		return 0, nil
	}
	return clampPercent(100 * float64(total-free) / float64(total)), nil
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
