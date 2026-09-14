package executor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/errdefs"
	"gopkg.in/yaml.v3"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/protocol"
)

type projectMigrationDocker interface {
	ContainerList(context.Context, container.ListOptions) ([]container.Summary, error)
	ContainerInspect(context.Context, string) (container.InspectResponse, error)
	ContainerStop(context.Context, string, container.StopOptions) error
	ContainerRemove(context.Context, string, container.RemoveOptions) error
	VolumeRemove(context.Context, string, bool) error
}

var (
	newProjectMigrationClient = func() (projectMigrationDocker, func(), error) {
		client, err := docker.NewClient()
		if err != nil {
			return nil, func() {}, err
		}
		return client.Raw(), func() { _ = client.Close() }, nil
	}
	runProjectMigrationUp   = compose.RunForceUp
	runProjectMigrationBack = compose.RunUp
)

func emitMigrationLine(onLine func(string), format string, args ...interface{}) {
	if onLine != nil {
		onLine("[project-migration] " + fmt.Sprintf(format, args...))
	}
}

func decodePreviousCompose(workDir string, spec *protocol.ProjectMigrationSpec) (string, []byte, error) {
	content, err := base64.StdEncoding.DecodeString(spec.PreviousComposeB64)
	if err != nil {
		return "", nil, fmt.Errorf("invalid previous compose revision: %w", err)
	}
	previousName, err := compose.ExtractProjectName(content)
	if err != nil {
		return "", nil, err
	}
	if previousName != spec.PreviousProjectName {
		return "", nil, fmt.Errorf("previous compose identity mismatch: revision declares %q, command expected %q", previousName, spec.PreviousProjectName)
	}
	path := filepath.Join(workDir, "previous-compose.yml")
	if err := os.WriteFile(path, content, 0600); err != nil {
		return "", nil, fmt.Errorf("stage previous compose revision: %w", err)
	}
	return filepath.Base(path), content, nil
}

func explicitContainerNames(composeContent []byte) ([]string, error) {
	var doc struct {
		Services map[string]struct {
			ContainerName string `yaml:"container_name"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(composeContent, &doc); err != nil {
		return nil, fmt.Errorf("parse target compose container names: %w", err)
	}
	names := make([]string, 0)
	for _, service := range doc.Services {
		if service.ContainerName != "" {
			names = append(names, service.ContainerName)
		}
	}
	sort.Strings(names)
	return names, nil
}

func namedVolumeNames(composeContent []byte) (map[string]struct{}, error) {
	var doc struct {
		Volumes map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(composeContent, &doc); err != nil {
		return nil, fmt.Errorf("parse deployed compose volumes: %w", err)
	}
	names := make(map[string]struct{}, len(doc.Volumes))
	for _, volume := range doc.Volumes {
		if volume.Name != "" {
			names[volume.Name] = struct{}{}
		}
	}
	return names, nil
}

func removableNamedVolumeNames(composeContent []byte) ([]string, error) {
	var doc struct {
		Volumes map[string]struct {
			Name     string `yaml:"name"`
			External bool   `yaml:"external"`
		} `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(composeContent, &doc); err != nil {
		return nil, fmt.Errorf("parse deployed compose volumes: %w", err)
	}
	names := make([]string, 0, len(doc.Volumes))
	for _, volume := range doc.Volumes {
		if volume.Name != "" && !volume.External {
			names = append(names, volume.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func listStackContainers(ctx context.Context, cli projectMigrationDocker, stackID string) ([]container.Summary, error) {
	f := filters.NewArgs()
	f.Add("label", "dev.wireops.stack_id="+stackID)
	return cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
}

func preflightProjectMigration(ctx context.Context, cli projectMigrationDocker, stackID, previousProject, targetProject string, targetCompose, previousCompose []byte, recreateVolumes bool) ([]container.Summary, error) {
	containerNames, err := explicitContainerNames(targetCompose)
	if err != nil {
		return nil, err
	}
	for _, name := range containerNames {
		inspected, err := cli.ContainerInspect(ctx, name)
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect target container name %q: %w", name, err)
		}
		owner := ""
		if inspected.Config != nil {
			owner = inspected.Config.Labels["dev.wireops.stack_id"]
		}
		if owner != stackID {
			return nil, fmt.Errorf("container name %q is owned by another workload; refusing project migration", name)
		}
	}

	owned, err := listStackContainers(ctx, cli, stackID)
	if err != nil {
		return nil, fmt.Errorf("list stack containers: %w", err)
	}
	namedVolumes, err := namedVolumeNames(previousCompose)
	if err != nil {
		return nil, err
	}
	for _, summary := range owned {
		project := summary.Labels["com.docker.compose.project"]
		if project != previousProject && project != targetProject {
			return nil, fmt.Errorf("container %.12s belongs to unexpected Compose project %q; refusing project migration", summary.ID, project)
		}
		if recreateVolumes {
			continue
		}
		inspected, err := cli.ContainerInspect(ctx, summary.ID)
		if err != nil {
			return nil, fmt.Errorf("inspect stack container %.12s: %w", summary.ID, err)
		}
		for _, point := range inspected.Mounts {
			if point.Type != mount.TypeVolume {
				continue
			}
			if _, named := namedVolumes[point.Name]; !named {
				return nil, fmt.Errorf("container %.12s uses anonymous volume %q at %s; enable volume recreation or convert it to a named volume before migrating", summary.ID, point.Name, point.Destination)
			}
		}
	}
	return owned, nil
}

func removeStackContainers(ctx context.Context, cli projectMigrationDocker, containers []container.Summary) error {
	var failures []string
	for _, summary := range containers {
		if err := cli.ContainerStop(ctx, summary.ID, container.StopOptions{}); err != nil && !errdefs.IsNotFound(err) {
			failures = append(failures, fmt.Sprintf("stop %.12s: %v", summary.ID, err))
			continue
		}
		if err := cli.ContainerRemove(ctx, summary.ID, container.RemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			failures = append(failures, fmt.Sprintf("remove %.12s: %v", summary.ID, err))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func removeNamedVolumes(ctx context.Context, cli projectMigrationDocker, previousCompose []byte) error {
	names, err := removableNamedVolumeNames(previousCompose)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := cli.VolumeRemove(ctx, name, false); err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("remove named volume %q: %w", name, err)
		}
	}
	return nil
}

func runProjectIdentityMigration(ctx context.Context, opts compose.ForceUpOptions, spec *protocol.ProjectMigrationSpec, stackID string) (string, error) {
	if stackID == "" {
		return "", errors.New("project identity migration requires a stack_id ownership boundary")
	}
	if spec == nil || spec.PreviousProjectName == "" || spec.TargetProjectName == "" || spec.PreviousProjectName == spec.TargetProjectName {
		return "", errors.New("project identity migration requires distinct non-empty project identities")
	}
	previousFile, previousCompose, err := decodePreviousCompose(opts.WorkDir, spec)
	if err != nil {
		return "", err
	}
	targetCompose, err := os.ReadFile(filepath.Join(opts.WorkDir, opts.ComposeFile))
	if err != nil {
		return "", fmt.Errorf("read target compose revision: %w", err)
	}
	targetName, err := compose.ExtractProjectName(targetCompose)
	if err != nil {
		return "", err
	}
	if targetName != spec.TargetProjectName {
		return "", fmt.Errorf("target compose identity mismatch: revision declares %q, command expected %q", targetName, spec.TargetProjectName)
	}

	client, cleanupClient, err := newProjectMigrationClient()
	if err != nil {
		return "", err
	}
	defer cleanupClient()
	restorePrevious := func(cause error, output string) (string, error) {
		emitMigrationLine(opts.OnLine, "migration interrupted; restoring previous Compose project %s", spec.PreviousProjectName)
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var cleanupErr error
		partial, listErr := listStackContainers(rollbackCtx, client, stackID)
		if listErr != nil {
			cleanupErr = fmt.Errorf("list partial migration containers: %w", listErr)
		} else if err := removeStackContainers(rollbackCtx, client, partial); err != nil {
			cleanupErr = fmt.Errorf("remove partial migration containers: %w", err)
		}
		rollbackOutput, rollbackErr := runProjectMigrationBack(rollbackCtx, compose.RunOptions{
			WorkDir:         opts.WorkDir,
			ComposeFile:     previousFile,
			RemoveOrphans:   false,
			DockerConfigDir: opts.DockerConfigDir,
			OnLine:          opts.OnLine,
		})
		combined := output + rollbackOutput
		if rollbackErr != nil || cleanupErr != nil {
			return combined, fmt.Errorf("project identity migration failed: %v; rollback failed: %v", cause, errors.Join(cleanupErr, rollbackErr))
		}
		emitMigrationLine(opts.OnLine, "previous Compose project restored")
		return combined, fmt.Errorf("project identity migration failed and was rolled back: %w", cause)
	}

	emitMigrationLine(opts.OnLine, "preflight %s -> %s", spec.PreviousProjectName, spec.TargetProjectName)
	owned, err := preflightProjectMigration(ctx, client, stackID, spec.PreviousProjectName, spec.TargetProjectName, targetCompose, previousCompose, opts.RecreateVolumes)
	if err != nil {
		return "", fmt.Errorf("project identity migration preflight failed: %w", err)
	}
	emitMigrationLine(opts.OnLine, "removing %d container(s) owned by this stack", len(owned))
	if err := removeStackContainers(ctx, client, owned); err != nil {
		return restorePrevious(fmt.Errorf("could not remove stack containers: %w", err), "")
	}
	if opts.RecreateVolumes {
		emitMigrationLine(opts.OnLine, "removing named volumes explicitly selected for recreation")
		if err := removeNamedVolumes(ctx, client, previousCompose); err != nil {
			return restorePrevious(fmt.Errorf("could not recreate volumes: %w", err), "")
		}
	}

	// Never run compose down for a project-name migration: the previous
	// identity may be shared by another legacy stack. New networks are created
	// by up; old networks remain untouched, and named volumes remain unless the
	// operator explicitly selected their recreation above.
	upOpts := opts
	upOpts.RecreateNetworks = false
	emitMigrationLine(opts.OnLine, "starting target Compose project %s", spec.TargetProjectName)
	output, upErr := runProjectMigrationUp(ctx, upOpts)
	if upErr == nil {
		emitMigrationLine(opts.OnLine, "project identity migration completed")
		return output, nil
	}

	return restorePrevious(upErr, output)
}
