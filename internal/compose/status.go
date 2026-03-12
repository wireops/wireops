package compose

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
	"github.com/wireops/wireops/internal/protocol"
)

type ServiceStatus struct {
	ServiceName   string
	ContainerID   string
	ContainerName string
	Status        string
	Labels        map[string]string
}

type ContainerStats struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsage   uint64  `json:"mem_usage"`
	MemLimit   uint64  `json:"mem_limit"`
	StartedAt  string  `json:"started_at"`
}

func GetStackStatus(ctx context.Context, cli *dockerclient.Client, projectName string) ([]ServiceStatus, error) {
	f := filters.NewArgs()
	f.Add("label", "com.docker.compose.project="+projectName)

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return nil, err
	}

	statuses := make([]ServiceStatus, 0, len(containers))
	for _, c := range containers {
		serviceName := c.Labels["com.docker.compose.service"]
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		cID := c.ID
		if len(cID) > 12 {
			cID = cID[:12]
		}

		statuses = append(statuses, ServiceStatus{
			ServiceName:   serviceName,
			ContainerID:   cID,
			ContainerName: name,
			Status:        mapStatus(c.State),
			Labels:        c.Labels,
		})
	}

	return statuses, nil
}

func GetContainerStats(ctx context.Context, cli *dockerclient.Client, containerID string) (*ContainerStats, error) {
	resp, err := cli.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw struct {
		CPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs     uint32 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64 `json:"usage"`
			Limit uint64 `json:"limit"`
		} `json:"memory_stats"`
		Networks map[string]struct {
			RxBytes uint64 `json:"rx_bytes"`
			TxBytes uint64 `json:"tx_bytes"`
		} `json:"networks"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	var cpuPercent float64
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage - raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemCPUUsage - raw.PreCPUStats.SystemCPUUsage)
	if sysDelta > 0 && raw.CPUStats.OnlineCPUs > 0 {
		cpuPercent = (cpuDelta / sysDelta) * float64(raw.CPUStats.OnlineCPUs) * 100.0
	}

	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		log.Printf("failed to inspect container %s: %v", containerID, err)
	}
	var startedAt string
	if err == nil && inspect.State != nil {
		startedAt = inspect.State.StartedAt
	}

	return &ContainerStats{
		CPUPercent: cpuPercent,
		MemUsage:   raw.MemoryStats.Usage,
		MemLimit:   raw.MemoryStats.Limit,
		StartedAt:  startedAt,
	}, nil
}

// GetStackVolumes returns Docker volumes associated with a compose project.
func GetStackVolumes(ctx context.Context, cli *dockerclient.Client, projectName string) ([]protocol.VolumeInfo, error) {
	f := filters.NewArgs()
	f.Add("label", "com.docker.compose.project="+projectName)

	resp, err := cli.VolumeList(ctx, volume.ListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	infos := make([]protocol.VolumeInfo, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		name := v.Labels["com.docker.compose.volume"]
		if name == "" {
			name = v.Name
		}
		infos = append(infos, protocol.VolumeInfo{
			Name:       name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Scope:      v.Scope,
		})
	}
	return infos, nil
}

// GetStackNetworks returns Docker networks associated with a compose project.
func GetStackNetworks(ctx context.Context, cli *dockerclient.Client, projectName string) ([]protocol.NetworkInfo, error) {
	f := filters.NewArgs()
	f.Add("label", "com.docker.compose.project="+projectName)

	networks, err := cli.NetworkList(ctx, dockernetwork.ListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	infos := make([]protocol.NetworkInfo, 0, len(networks))
	for _, n := range networks {
		info := protocol.NetworkInfo{
			Name:   n.Labels["com.docker.compose.network"],
			Driver: n.Driver,
			Scope:  n.Scope,
		}
		if info.Name == "" {
			info.Name = n.Name
		}
		if len(n.IPAM.Config) > 0 {
			info.Subnet = n.IPAM.Config[0].Subnet
			info.Gateway = n.IPAM.Config[0].Gateway
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// mapStatus translates Docker container states into normalized application statuses.
func mapStatus(state string) string {
	switch strings.ToLower(state) {
	case "running":
		return "running"
	case "exited", "dead":
		return "exited"
	case "paused":
		return "paused"
	case "created":
		return "created"
	case "restarting":
		return "error"
	default:
		return "missing"
	}
}

// ContainerBelongsToProject checks if a container belongs to the specified compose project.
func ContainerBelongsToProject(ctx context.Context, cli *dockerclient.Client, containerID string, projectName string) (bool, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, err
	}
	if inspect.Config == nil {
		return false, nil
	}
	return inspect.Config.Labels["com.docker.compose.project"] == projectName, nil
}
