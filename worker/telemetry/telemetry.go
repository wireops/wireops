package telemetry

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/protocol"
)

var (
	CachedWorkerInfo *protocol.WorkerInfo
	lastCPUTotal     uint64
	lastCPUIdle      uint64
)

func InitWorkerInfo() {
	CachedWorkerInfo = &protocol.WorkerInfo{
		DockerVersion:  QueryDockerVersion(),
		ComposeVersion: QueryComposeVersion(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
	}
}

func QueryDockerVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dockerPath, err := lookPathSecure("docker")
	if err != nil {
		return ""
	}
	cmd := exec.CommandContext(ctx, dockerPath, "version", "--format", "{{.Server.Version}}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func QueryComposeVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dockerPath, err := lookPathSecure("docker")
	if err != nil {
		return ""
	}
	cmd := exec.CommandContext(ctx, dockerPath, "compose", "version", "--short")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func GetTelemetry(stackDir string) *protocol.TelemetryInfo {
	info := &protocol.TelemetryInfo{
		DockerOnline: false,
	}

	// 1. Check Docker daemon connectivity
	if cli, err := docker.NewClient(); err == nil {
		info.DockerOnline = true
		_ = cli.Close()
	}

	// 2. CPU Usage
	if runtime.GOOS == "linux" {
		if file, err := os.Open("/proc/stat"); err == nil {
			scanner := bufio.NewScanner(file)
			if scanner.Scan() {
				line := scanner.Text()
				fields := strings.Fields(line)
				if len(fields) >= 5 && fields[0] == "cpu" {
					var total, idle uint64
					for i, field := range fields[1:] {
						val, _ := strconv.ParseUint(field, 10, 64)
						total += val
						if i == 3 { // idle field
							idle = val
						}
					}
					if lastCPUTotal > 0 {
						deltaTotal := total - lastCPUTotal
						deltaIdle := idle - lastCPUIdle
						if deltaTotal > 0 {
							info.CPUUsagePercent = float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100.0
						}
					}
					lastCPUTotal = total
					lastCPUIdle = idle
				}
			}
			_ = file.Close()
		}
	} else {
		// Mock CPU for Darwin development
		info.CPUUsagePercent = 5.0
	}

	// 3. Memory Usage
	if runtime.GOOS == "linux" {
		if file, err := os.Open("/proc/meminfo"); err == nil {
			scanner := bufio.NewScanner(file)
			var totalMem, availMem float64
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) >= 2 {
					if fields[0] == "MemTotal:" {
						totalMem, _ = strconv.ParseFloat(fields[1], 64)
					} else if fields[0] == "MemAvailable:" {
						availMem, _ = strconv.ParseFloat(fields[1], 64)
					}
				}
			}
			_ = file.Close()
			if totalMem > 0 {
				usedMem := totalMem - availMem
				info.MemoryUsagePercent = (usedMem / totalMem) * 100.0
			}
		}
	} else {
		// Mock Memory for Darwin development
		info.MemoryUsagePercent = 45.0
	}

	// 4. Disk Usage
	_ = os.MkdirAll(stackDir, 0700)
	var stat syscall.Statfs_t
	if err := syscall.Statfs(stackDir, &stat); err == nil {
		totalDisk := stat.Blocks * uint64(stat.Bsize)
		freeDisk := stat.Bavail * uint64(stat.Bsize)
		if totalDisk > 0 {
			usedDisk := totalDisk - freeDisk
			info.DiskUsagePercent = float64(usedDisk) / float64(totalDisk) * 100.0
		}
	}

	return info
}

func lookPathSecure(file string) (string, error) {
	safeDirs := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin"}
	for _, dir := range safeDirs {
		path := filepath.Join(dir, file)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}
