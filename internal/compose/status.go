package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/netip"
	"path"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	dockerclient "github.com/moby/moby/client"
	"github.com/wireops/wireops/internal/protocol"
)

type ServiceStatus struct {
	ServiceName   string
	ContainerID   string
	ContainerName string
	Status        string
	Labels        map[string]string

	// Health is the Docker healthcheck status ("healthy", "unhealthy",
	// "starting") when the container defines a healthcheck, or "none" when
	// it doesn't. Populated via a per-container inspect since the list API
	// doesn't expose structured health.
	Health string

	// RestartCount is the number of times Docker has restarted this
	// container (visible via `docker inspect`), used to detect restart loops.
	RestartCount int

	// StartedAt is the RFC3339 timestamp of the container's last start,
	// used alongside RestartCount to judge whether restarts are recent.
	StartedAt string

	// ExitCode is the container's last exit code (from `docker inspect`
	// State.ExitCode), meaningful only once the container has exited. Used to
	// distinguish a cleanly-completed one-shot/init container (0) from a crash
	// (non-zero) — otherwise both map to Status "exited". -1 means the exit
	// code couldn't be determined (the per-container inspect failed) — must
	// not be read as a clean exit.
	ExitCode int

	// Ports lists published/exposed container ports, mapped from the
	// Docker API's container.Summary.Ports. Nil when the container
	// defines no ports.
	Ports []protocol.PortInfo
}

type ContainerStats struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsage   uint64  `json:"mem_usage"`
	MemLimit   uint64  `json:"mem_limit"`
	StartedAt  string  `json:"started_at"`
}

func GetStackStatus(ctx context.Context, cli *dockerclient.Client, projectName string) ([]ServiceStatus, error) {
	return GetStackStatusForStack(ctx, cli, projectName, "")
}

// GetStackStatusForStack narrows a project query to WireOps' immutable stack
// label when available. This prevents legacy stacks that once shared a
// Compose project name from seeing each other's containers.
func GetStackStatusForStack(ctx context.Context, cli *dockerclient.Client, projectName, stackID string) ([]ServiceStatus, error) {
	f := dockerclient.Filters{}
	f.Add("label", "com.docker.compose.project="+projectName)
	if stackID != "" {
		f.Add("label", "dev.wireops.stack_id="+stackID)
	}

	containers, err := cli.ContainerList(ctx, dockerclient.ContainerListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return nil, err
	}

	statuses := make([]ServiceStatus, 0, len(containers.Items))
	for _, c := range containers.Items {
		serviceName := c.Labels["com.docker.compose.service"]
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		cID := c.ID
		if len(cID) > 12 {
			cID = cID[:12]
		}

		health, restartCount, startedAt, exitCode := inspectHealthAndRestarts(ctx, cli, c.ID)

		statuses = append(statuses, ServiceStatus{
			ServiceName:   serviceName,
			ContainerID:   cID,
			ContainerName: name,
			Status:        mapStatus(string(c.State)),
			Labels:        filterIntegrationLabels(c.Labels),
			Health:        health,
			RestartCount:  restartCount,
			StartedAt:     startedAt,
			ExitCode:      exitCode,
			Ports:         mapDockerPorts(c.Ports),
		})
	}

	return statuses, nil
}

// mapDockerPorts converts the Docker API's container.PortSummary slice (from
// container.Summary.Ports) into the wire-stable protocol.PortInfo slice.
// Ports with PublicPort == 0 are exposed but not published to the host.
func mapDockerPorts(ports []container.PortSummary) []protocol.PortInfo {
	if len(ports) == 0 {
		return nil
	}
	infos := make([]protocol.PortInfo, 0, len(ports))
	for _, p := range ports {
		infos = append(infos, protocol.PortInfo{
			ContainerPort: p.PrivatePort,
			Protocol:      p.Type,
			HostIP:        addrString(p.IP),
			HostPort:      p.PublicPort,
		})
	}
	return infos
}

// addrString renders an address the way the pre-netip Docker API types carried
// it: empty when the daemon didn't send one.
func addrString(a netip.Addr) string {
	if !a.IsValid() {
		return ""
	}
	return a.String()
}

func prefixString(p netip.Prefix) string {
	if !p.IsValid() {
		return ""
	}
	return p.String()
}

// inspectHealthAndRestarts reads the structured healthcheck status and
// restart count for a single container. Errors are swallowed (returning
// zero values) since this augments best-effort status reporting and must
// not fail the whole status listing if one container disappears mid-inspect.
func inspectHealthAndRestarts(ctx context.Context, cli *dockerclient.Client, containerID string) (health string, restartCount int, startedAt string, exitCode int) {
	res, err := cli.ContainerInspect(ctx, containerID, dockerclient.ContainerInspectOptions{})
	if err != nil || res.Container.State == nil {
		// -1: unknown, not a clean exit — see ServiceStatus.ExitCode.
		return "none", 0, "", -1
	}

	inspect := res.Container
	health = "none"
	if inspect.State.Health != nil {
		health = string(inspect.State.Health.Status)
	}

	return health, inspect.RestartCount, inspect.State.StartedAt, inspect.State.ExitCode
}

func GetContainerStats(ctx context.Context, cli *dockerclient.Client, containerID string) (*ContainerStats, error) {
	resp, err := cli.ContainerStats(ctx, containerID, dockerclient.ContainerStatsOptions{Stream: false})
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

	inspect, err := cli.ContainerInspect(ctx, containerID, dockerclient.ContainerInspectOptions{})
	if err != nil {
		log.Printf("failed to inspect container %s: %v", containerID, err)
	}
	var startedAt string
	if err == nil && inspect.Container.State != nil {
		startedAt = inspect.Container.State.StartedAt
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
	f := dockerclient.Filters{}
	f.Add("label", "com.docker.compose.project="+projectName)

	resp, err := cli.VolumeList(ctx, dockerclient.VolumeListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	// VolumeInspect may omit UsageData depending on the daemon API version. The
	// daemon's disk-usage endpoint calculates it itself (equivalent to `docker
	// system df -v`) and remains accessible through the worker's Docker socket.
	// Scoped to volumes only so this doesn't also compute container/image/
	// build-cache usage on every call.
	diskUsageSizes := map[string]int64{}
	// Verbose is what makes the daemon return the per-volume items, not just
	// the totals.
	diskUsageOpts := dockerclient.DiskUsageOptions{Volumes: true, Verbose: true}
	if diskUsage, diskUsageErr := cli.DiskUsage(ctx, diskUsageOpts); diskUsageErr == nil {
		diskUsageSizes = volumeUsageSizes(diskUsage.Volumes.Items)
	}

	infos := make([]protocol.VolumeInfo, 0, len(resp.Items))
	for _, v := range resp.Items {
		// UsageData is not consistently returned by VolumeList. Only fall back to
		// inspecting the individual volume when disk-usage didn't already report
		// its size, since inspect is a per-volume daemon round trip.
		if _, hasUsage := diskUsageSizes[v.Name]; v.UsageData == nil && !hasUsage {
			if inspected, inspectErr := cli.VolumeInspect(ctx, v.Name, dockerclient.VolumeInspectOptions{}); inspectErr == nil {
				v = inspected.Volume
			} else {
				log.Printf("failed to inspect volume %s: %v", v.Name, inspectErr)
			}
		}

		name := v.Labels["com.docker.compose.volume"]
		if name == "" {
			name = v.Name
		}
		info := protocol.VolumeInfo{
			Name:       name,
			DockerName: v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Scope:      v.Scope,
			CreatedAt:  v.CreatedAt,
			Options:    v.Options,
		}
		if v.UsageData != nil && v.UsageData.Size >= 0 {
			size := v.UsageData.Size
			info.SizeBytes = &size
		} else if size, ok := diskUsageSizes[v.Name]; ok {
			info.SizeBytes = &size
		}
		infos = append(infos, info)
	}

	bindMounts, err := getStackBindMounts(ctx, cli, projectName)
	if err != nil {
		return infos, fmt.Errorf("inspect bind mounts: %w", err)
	}
	infos = append(infos, bindMounts...)

	return infos, nil
}

// getStackBindMounts discovers host bind mounts used by the project's
// containers. Bind mounts are never Docker-managed volumes - they don't
// appear in `docker volume ls` - so the only way to find them is inspecting
// each container's actual Mounts.
func getStackBindMounts(ctx context.Context, cli *dockerclient.Client, projectName string) ([]protocol.VolumeInfo, error) {
	f := dockerclient.Filters{}
	f.Add("label", "com.docker.compose.project="+projectName)

	containers, err := cli.ContainerList(ctx, dockerclient.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var infos []protocol.VolumeInfo
	for _, c := range containers.Items {
		res, ierr := cli.ContainerInspect(ctx, c.ID, dockerclient.ContainerInspectOptions{})
		if ierr != nil {
			if errdefs.IsNotFound(ierr) {
				continue
			}
			return infos, fmt.Errorf("inspect container %s for bind mounts: %w", c.ID, ierr)
		}
		for _, m := range res.Container.Mounts {
			if m.Type != mount.TypeBind {
				continue
			}
			key := m.Source + "|" + m.Destination
			if seen[key] {
				continue
			}
			seen[key] = true
			infos = append(infos, protocol.VolumeInfo{
				Name:        m.Source,
				DockerName:  m.Source,
				Driver:      "bind",
				Mountpoint:  m.Source,
				Scope:       "local",
				Type:        "bind",
				Destination: m.Destination,
			})
		}
	}
	return infos, nil
}

func volumeUsageSizes(volumes []volume.Volume) map[string]int64 {
	sizes := make(map[string]int64, len(volumes))
	for _, v := range volumes {
		if v.UsageData != nil && v.UsageData.Size >= 0 {
			sizes[v.Name] = v.UsageData.Size
		}
	}
	return sizes
}

// GetStackNetworks returns Docker networks associated with a compose project.
func GetStackNetworks(ctx context.Context, cli *dockerclient.Client, projectName string) ([]protocol.NetworkInfo, error) {
	f := dockerclient.Filters{}
	f.Add("label", "com.docker.compose.project="+projectName)

	networks, err := cli.NetworkList(ctx, dockerclient.NetworkListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	infos := make([]protocol.NetworkInfo, 0, len(networks.Items))
	seenIDs := make(map[string]bool, len(networks.Items))
	for _, n := range networks.Items {
		infos = append(infos, buildNetworkInfo(n.Network, false))
		seenIDs[n.ID] = true
	}

	externalInfos, err := getExternalStackNetworks(ctx, cli, projectName, seenIDs)
	if err != nil {
		return infos, fmt.Errorf("inspect external networks: %w", err)
	}
	infos = append(infos, externalInfos...)

	return infos, nil
}

func buildNetworkInfo(n dockernetwork.Network, external bool) protocol.NetworkInfo {
	info := protocol.NetworkInfo{
		Name:       n.Labels["com.docker.compose.network"],
		DockerName: n.Name,
		ID:         n.ID,
		Driver:     n.Driver,
		Scope:      n.Scope,
		EnableIPv4: n.EnableIPv4,
		EnableIPv6: n.EnableIPv6,
		Internal:   n.Internal,
		Attachable: n.Attachable,
		Ingress:    n.Ingress,
		ConfigOnly: n.ConfigOnly,
		Options:    n.Options,
		External:   external,
	}
	if !n.Created.IsZero() {
		info.CreatedAt = n.Created.Format(time.RFC3339)
	}
	if info.Name == "" {
		info.Name = n.Name
	}
	for _, config := range n.IPAM.Config {
		var aux map[string]string
		if len(config.AuxAddress) > 0 {
			aux = make(map[string]string, len(config.AuxAddress))
			for host, addr := range config.AuxAddress {
				aux[host] = addrString(addr)
			}
		}
		info.IPAMConfigs = append(info.IPAMConfigs, protocol.NetworkIPAMConfig{
			Subnet:       prefixString(config.Subnet),
			Gateway:      addrString(config.Gateway),
			IPRange:      prefixString(config.IPRange),
			AuxAddresses: aux,
		})
	}
	if len(info.IPAMConfigs) > 0 {
		info.Subnet = info.IPAMConfigs[0].Subnet
		info.Gateway = info.IPAMConfigs[0].Gateway
	}
	return info
}

// getExternalStackNetworks finds networks the project's containers are
// actually attached to but that aren't owned by this compose project (i.e.
// declared `external: true` in compose). Those networks pre-exist outside
// the project and never receive the com.docker.compose.project label, so
// they can only be found by walking the containers' real network
// attachments rather than filtering NetworkList by label.
func getExternalStackNetworks(ctx context.Context, cli *dockerclient.Client, projectName string, seenNetworkIDs map[string]bool) ([]protocol.NetworkInfo, error) {
	f := dockerclient.Filters{}
	f.Add("label", "com.docker.compose.project="+projectName)

	containers, err := cli.ContainerList(ctx, dockerclient.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}

	seenNames := make(map[string]bool)
	var infos []protocol.NetworkInfo
	for _, c := range containers.Items {
		res, ierr := cli.ContainerInspect(ctx, c.ID, dockerclient.ContainerInspectOptions{})
		if ierr != nil {
			if errdefs.IsNotFound(ierr) {
				continue
			}
			return infos, fmt.Errorf("inspect container %s for networks: %w", c.ID, ierr)
		}
		inspect := res.Container
		if inspect.NetworkSettings == nil {
			return infos, fmt.Errorf("inspect container %s for networks: no network settings", c.ID)
		}
		for netName, endpoint := range inspect.NetworkSettings.Networks {
			networkID := netName
			if endpoint != nil && endpoint.NetworkID != "" {
				networkID = endpoint.NetworkID
			}
			if networkID == "" || seenNames[networkID] {
				continue
			}
			seenNames[networkID] = true
			if endpoint != nil && seenNetworkIDs[endpoint.NetworkID] {
				continue
			}

			netRes, nerr := cli.NetworkInspect(ctx, networkID, dockerclient.NetworkInspectOptions{})
			if nerr != nil {
				if errdefs.IsNotFound(nerr) {
					continue
				}
				return infos, fmt.Errorf("inspect external network %s: %w", netName, nerr)
			}
			netInspect := netRes.Network.Network
			if seenNetworkIDs[netInspect.ID] {
				continue
			}
			seenNetworkIDs[netInspect.ID] = true
			infos = append(infos, buildNetworkInfo(netInspect, true))
		}
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
	res, err := cli.ContainerInspect(ctx, containerID, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return false, err
	}
	if res.Container.Config == nil {
		return false, nil
	}
	return res.Container.Config.Labels["com.docker.compose.project"] == projectName, nil
}

// ContainerBelongsToStack checks the immutable WireOps ownership label. It is
// stronger than Compose project membership because old releases could assign
// the same project name to multiple stacks.
func ContainerBelongsToStack(ctx context.Context, cli *dockerclient.Client, containerID, stackID string) (bool, error) {
	res, err := cli.ContainerInspect(ctx, containerID, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return false, err
	}
	if res.Container.Config == nil {
		return false, nil
	}
	return res.Container.Config.Labels["dev.wireops.stack_id"] == stackID, nil
}

// filterIntegrationLabels restricts labels to only those relevant for integrations (e.g. traefik., dozzle.)
func filterIntegrationLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}

	allowlist := []string{
		"traefik.http.routers.*",
		"traefik.enable",
		"dozzle.enable",
		"customization.image.slug",
		"customization.init",
	}

	filtered := make(map[string]string)
	for k, v := range labels {
		lk := strings.ToLower(k)
		for _, pattern := range allowlist {
			if matched, _ := path.Match(pattern, lk); matched {
				filtered[k] = v
				break
			}
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
