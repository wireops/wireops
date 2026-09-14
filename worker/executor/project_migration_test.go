package executor

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/errdefs"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/protocol"
)

type fakeProjectMigrationClient struct {
	containers []container.Summary
	inspects   map[string]container.InspectResponse
	stopped    []string
	removed    []string
	volumes    []string
}

func (f *fakeProjectMigrationClient) ContainerList(_ context.Context, opts container.ListOptions) ([]container.Summary, error) {
	stackID := ""
	for _, label := range opts.Filters.Get("label") {
		if strings.HasPrefix(label, "dev.wireops.stack_id=") {
			stackID = strings.TrimPrefix(label, "dev.wireops.stack_id=")
		}
	}
	result := make([]container.Summary, 0, len(f.containers))
	for _, summary := range f.containers {
		if stackID == "" || summary.Labels["dev.wireops.stack_id"] == stackID {
			result = append(result, summary)
		}
	}
	return result, nil
}

func (f *fakeProjectMigrationClient) ContainerInspect(_ context.Context, id string) (container.InspectResponse, error) {
	if response, ok := f.inspects[id]; ok {
		return response, nil
	}
	return container.InspectResponse{}, errdefs.NotFound(errors.New("not found"))
}

func (f *fakeProjectMigrationClient) ContainerStop(_ context.Context, id string, _ container.StopOptions) error {
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeProjectMigrationClient) ContainerRemove(_ context.Context, id string, _ container.RemoveOptions) error {
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeProjectMigrationClient) VolumeRemove(_ context.Context, name string, _ bool) error {
	f.volumes = append(f.volumes, name)
	return nil
}

func (f *fakeProjectMigrationClient) Close() error { return nil }

func migrationCompose(name string) []byte {
	return []byte("name: " + name + "\nservices:\n  pihole:\n    image: pihole/pihole:latest\n    container_name: pihole\nvolumes:\n  data:\n    name: red_data\n")
}

func ownedInspect(stackID string, mounts ...container.MountPoint) container.InspectResponse {
	return container.InspectResponse{Config: &container.Config{Labels: map[string]string{"dev.wireops.stack_id": stackID}}, Mounts: mounts}
}

func TestPreflightProjectMigration(t *testing.T) {
	const stackID = "stack-pihole"
	oldCompose := migrationCompose("red")
	targetCompose := migrationCompose("pihole-red")

	t.Run("accepts own container and named volume", func(t *testing.T) {
		fake := &fakeProjectMigrationClient{
			containers: []container.Summary{{ID: "owned", Labels: map[string]string{"com.docker.compose.project": "red", "dev.wireops.stack_id": stackID}}},
			inspects: map[string]container.InspectResponse{
				"pihole": ownedInspect(stackID),
				"owned":  ownedInspect(stackID, container.MountPoint{Type: mount.TypeVolume, Name: "red_data", Destination: "/etc/pihole"}),
			},
		}
		owned, err := preflightProjectMigration(context.Background(), fake, stackID, "red", "pihole-red", targetCompose, oldCompose, false)
		if err != nil || len(owned) != 1 || owned[0].ID != "owned" {
			t.Fatalf("owned=%v err=%v", owned, err)
		}
	})

	t.Run("rejects foreign explicit container name", func(t *testing.T) {
		fake := &fakeProjectMigrationClient{inspects: map[string]container.InspectResponse{"pihole": ownedInspect("another-stack")}}
		_, err := preflightProjectMigration(context.Background(), fake, stackID, "red", "pihole-red", targetCompose, oldCompose, false)
		if err == nil || !strings.Contains(err.Error(), "another workload") {
			t.Fatalf("error = %v", err)
		}
		if len(fake.removed) != 0 {
			t.Fatal("preflight mutated containers")
		}
	})

	t.Run("rejects anonymous volume unless recreation requested", func(t *testing.T) {
		fake := &fakeProjectMigrationClient{
			containers: []container.Summary{{ID: "owned", Labels: map[string]string{"com.docker.compose.project": "red", "dev.wireops.stack_id": stackID}}},
			inspects: map[string]container.InspectResponse{
				"pihole": ownedInspect(stackID),
				"owned":  ownedInspect(stackID, container.MountPoint{Type: mount.TypeVolume, Name: "anonymous-id", Destination: "/tmp"}),
			},
		}
		_, err := preflightProjectMigration(context.Background(), fake, stackID, "red", "pihole-red", targetCompose, oldCompose, false)
		if err == nil || !strings.Contains(err.Error(), "anonymous volume") {
			t.Fatalf("error = %v", err)
		}
		if _, err := preflightProjectMigration(context.Background(), fake, stackID, "red", "pihole-red", targetCompose, oldCompose, true); err != nil {
			t.Fatalf("recreate volumes should allow migration: %v", err)
		}
	})
}

func TestRemoveNamedVolumesSkipsExternalVolumes(t *testing.T) {
	fake := &fakeProjectMigrationClient{}
	previous := []byte(`
volumes:
  data:
    name: legacy_data
  shared:
    name: shared_storage
    external: true
`)
	if err := removeNamedVolumes(context.Background(), fake, previous); err != nil {
		t.Fatal(err)
	}
	if len(fake.volumes) != 1 || fake.volumes[0] != "legacy_data" {
		t.Fatalf("removed volumes = %v, want only legacy_data", fake.volumes)
	}
}

func TestProjectIdentityMigrationRollsBackFailedTarget(t *testing.T) {
	const stackID = "stack-pihole"
	fake := &fakeProjectMigrationClient{
		containers: []container.Summary{{ID: "old-container", Labels: map[string]string{"com.docker.compose.project": "red", "dev.wireops.stack_id": stackID}}},
		inspects: map[string]container.InspectResponse{
			"pihole":        ownedInspect(stackID),
			"old-container": ownedInspect(stackID),
		},
	}
	oldFactory := newProjectMigrationClient
	oldUp := runProjectMigrationUp
	oldBack := runProjectMigrationBack
	t.Cleanup(func() {
		newProjectMigrationClient = oldFactory
		runProjectMigrationUp = oldUp
		runProjectMigrationBack = oldBack
	})
	newProjectMigrationClient = func() (projectMigrationDocker, func(), error) { return fake, func() {}, nil }
	runProjectMigrationUp = func(context.Context, compose.ForceUpOptions) (string, error) {
		return "target failed\n", errors.New("boom")
	}
	rolledBack := false
	runProjectMigrationBack = func(_ context.Context, opts compose.RunOptions) (string, error) {
		rolledBack = opts.ComposeFile == "previous-compose.yml" && !opts.RemoveOrphans
		return "restored\n", nil
	}

	workDir := t.TempDir()
	target := migrationCompose("pihole-red")
	if err := os.WriteFile(filepath.Join(workDir, "docker-compose.yml"), target, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := runProjectIdentityMigration(context.Background(), compose.ForceUpOptions{RunOptions: compose.RunOptions{WorkDir: workDir, ComposeFile: "docker-compose.yml"}}, &protocol.ProjectMigrationSpec{
		PreviousProjectName: "red",
		TargetProjectName:   "pihole-red",
		PreviousComposeB64:  base64.StdEncoding.EncodeToString(migrationCompose("red")),
	}, stackID)
	if err == nil || !strings.Contains(err.Error(), "rolled back") || !rolledBack {
		t.Fatalf("err=%v rolledBack=%v", err, rolledBack)
	}
	if len(fake.removed) == 0 || fake.removed[0] != "old-container" {
		t.Fatalf("removed = %v", fake.removed)
	}
}

func TestProjectIdentityMigrationRecreatesNamedVolumesWhenExplicitlyRequested(t *testing.T) {
	const stackID = "stack-pihole"
	fake := &fakeProjectMigrationClient{
		containers: []container.Summary{
			{ID: "old-container", Labels: map[string]string{"com.docker.compose.project": "red", "dev.wireops.stack_id": stackID}},
			{ID: "peer-container", Labels: map[string]string{"com.docker.compose.project": "red", "dev.wireops.stack_id": "peer-stack"}},
		},
		inspects: map[string]container.InspectResponse{
			"pihole":        ownedInspect(stackID),
			"old-container": ownedInspect(stackID),
		},
	}
	oldFactory := newProjectMigrationClient
	oldUp := runProjectMigrationUp
	t.Cleanup(func() {
		newProjectMigrationClient = oldFactory
		runProjectMigrationUp = oldUp
	})
	newProjectMigrationClient = func() (projectMigrationDocker, func(), error) { return fake, func() {}, nil }
	runProjectMigrationUp = func(_ context.Context, opts compose.ForceUpOptions) (string, error) {
		if opts.RecreateNetworks {
			t.Fatal("migration must not run compose down against the legacy project")
		}
		return "started\n", nil
	}

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "docker-compose.yml"), migrationCompose("pihole-red"), 0600); err != nil {
		t.Fatal(err)
	}
	var liveOutput []string
	_, err := runProjectIdentityMigration(context.Background(), compose.ForceUpOptions{
		RunOptions: compose.RunOptions{
			WorkDir:     workDir,
			ComposeFile: "docker-compose.yml",
			OnLine:      func(line string) { liveOutput = append(liveOutput, line) },
		},
		RecreateVolumes:  true,
		RecreateNetworks: true,
	}, &protocol.ProjectMigrationSpec{
		PreviousProjectName: "red",
		TargetProjectName:   "pihole-red",
		PreviousComposeB64:  base64.StdEncoding.EncodeToString(migrationCompose("red")),
	}, stackID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.volumes) != 1 || fake.volumes[0] != "red_data" {
		t.Fatalf("removed volumes = %v", fake.volumes)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "old-container" {
		t.Fatalf("migration touched a peer stack sharing the legacy project: %v", fake.removed)
	}
	joined := strings.Join(liveOutput, "\n")
	for _, phase := range []string{"preflight red -> pihole-red", "removing 1 container", "starting target Compose project pihole-red", "migration completed"} {
		if !strings.Contains(joined, phase) {
			t.Fatalf("live output missing %q:\n%s", phase, joined)
		}
	}
}
