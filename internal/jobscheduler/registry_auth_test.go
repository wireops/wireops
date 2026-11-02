package jobscheduler

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/registry"
)

const registryAuthTestSecret = "0123456789abcdef0123456789abcdef"

// ensureRegistryCredentialsCollection adds the optional registry_credentials
// collection that the base job scheduler test app omits.
func ensureRegistryCredentialsCollection(t *testing.T, app core.App) *core.Collection {
	t.Helper()
	col := core.NewBaseCollection("registry_credentials")
	col.Fields.Add(&core.TextField{Name: "name"})
	col.Fields.Add(&core.TextField{Name: "registry_url"})
	col.Fields.Add(&core.TextField{Name: "registry_host"})
	col.Fields.Add(&core.TextField{Name: "auth_type"})
	col.Fields.Add(&core.TextField{Name: "username"})
	col.Fields.Add(&core.TextField{Name: "password"})
	col.Fields.Add(&core.BoolField{Name: "insecure"})
	mustSaveCollection(t, app, col)
	return col
}

// addRegistryCredential stores a credential whose password is encrypted with
// registryAuthTestSecret, or stored verbatim when encrypt is false.
func addRegistryCredential(t *testing.T, app core.App, col *core.Collection, url, password string, insecure, encrypt bool) {
	t.Helper()
	stored := password
	if encrypt {
		encrypted, err := crypto.Encrypt([]byte(password), []byte(registryAuthTestSecret))
		if err != nil {
			t.Fatalf("encrypt password: %v", err)
		}
		stored = encrypted
	}
	record := core.NewRecord(col)
	record.Set("name", url)
	record.Set("registry_url", url)
	record.Set("registry_host", registry.NormalizeRegistryHost(url))
	record.Set("auth_type", "basic")
	record.Set("username", "user")
	record.Set("password", stored)
	record.Set("insecure", insecure)
	if err := app.Save(record); err != nil {
		t.Fatalf("save credential: %v", err)
	}
}

// setupRegistryAuthJob creates a job for image plus one connected worker and
// returns a scheduler whose dispatched RunJobCommands land on the channel.
func setupRegistryAuthJob(t *testing.T, app core.App, image string) (*Scheduler, string, chan protocol.RunJobCommand) {
	t.Helper()
	repo := createJobRepoRecord(t, app)
	writeJobFileWithImage(t, repo.Id, "job.yaml", image)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")
	worker := mustCreateRecord(t, app, "workers", map[string]any{
		"hostname":    "worker-1",
		"fingerprint": "fp1",
		"status":      "ACTIVE",
	})

	cmds := make(chan protocol.RunJobCommand, 1)
	dispatcher := capturingJobDispatcher{
		workers: []string{worker.Id},
		onDispatch: func(cmd interface{}) {
			if runJobCmd, ok := cmd.(protocol.RunJobCommand); ok {
				cmds <- runJobCmd
			}
		},
	}
	return NewScheduler(app, dispatcher, config.GetReposWorkspace()), jobRec.Id, cmds
}

// dispatchAndCapture runs the job and returns the dispatched command, waiting
// for the run to reach "running" so cleanup does not race the goroutine.
func dispatchAndCapture(t *testing.T, app core.App, s *Scheduler, jobID string, cmds chan protocol.RunJobCommand) protocol.RunJobCommand {
	t.Helper()
	s.executeJob(jobID, "manual", "test-user")

	var cmd protocol.RunJobCommand
	select {
	case cmd = <-cmds:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for job to be dispatched")
	}
	waitForJobRunStatus(t, app, cmd.JobRunID, "running")
	return cmd
}

func waitForJobRunStatus(t *testing.T, app core.App, runID, status string) *core.Record {
	t.Helper()
	pollStart := time.Now()
	for {
		run, err := app.FindRecordById("job_runs", runID)
		if err == nil && run.GetString("status") == status {
			return run
		}
		if time.Since(pollStart) > 2*time.Second {
			t.Fatalf("timed out waiting for job_run %s status to become %s", runID, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func decodeDockerAuthKeys(t *testing.T, authB64 string) []string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		t.Fatalf("decode registry auth: %v", err)
	}
	var cfg struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		t.Fatalf("unmarshal registry auth: %v", err)
	}
	keys := make([]string, 0, len(cfg.Auths))
	for key := range cfg.Auths {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestExecuteJobAttachesRegistryAuth(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	t.Setenv(constants.EnvSecretKey, registryAuthTestSecret)
	col := ensureRegistryCredentialsCollection(t, app)
	addRegistryCredential(t, app, col, "https://registry.example.com:5000", "one", true, true)
	addRegistryCredential(t, app, col, "ghcr.io", "two", false, true)

	s, jobID, cmds := setupRegistryAuthJob(t, app, "registry.example.com:5000/team/app:1.0")
	cmd := dispatchAndCapture(t, app, s, jobID, cmds)

	if cmd.RegistryAuthB64 == "" {
		t.Fatal("expected RegistryAuthB64 to be set")
	}
	if keys := decodeDockerAuthKeys(t, cmd.RegistryAuthB64); !slices.Equal(keys, []string{"registry.example.com:5000"}) {
		t.Errorf("auth keys = %v, want only registry.example.com:5000", keys)
	}
	if !slices.Equal(cmd.InsecureRegistries, []string{"registry.example.com:5000"}) {
		t.Errorf("InsecureRegistries = %v, want [registry.example.com:5000]", cmd.InsecureRegistries)
	}
}

func TestExecuteJobWithoutMatchingCredentialIsAnonymous(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	t.Setenv(constants.EnvSecretKey, registryAuthTestSecret)
	col := ensureRegistryCredentialsCollection(t, app)
	addRegistryCredential(t, app, col, "ghcr.io", "two", true, true)

	s, jobID, cmds := setupRegistryAuthJob(t, app, "registry.example.com:5000/team/app:1.0")
	cmd := dispatchAndCapture(t, app, s, jobID, cmds)

	if cmd.RegistryAuthB64 != "" {
		t.Errorf("RegistryAuthB64 = %q, want empty", cmd.RegistryAuthB64)
	}
	if len(cmd.InsecureRegistries) != 0 {
		t.Errorf("InsecureRegistries = %v, want empty", cmd.InsecureRegistries)
	}
}

func TestExecuteJobBareImageUsesDockerHubCredential(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	t.Setenv(constants.EnvSecretKey, registryAuthTestSecret)
	col := ensureRegistryCredentialsCollection(t, app)
	addRegistryCredential(t, app, col, "https://index.docker.io/v1/", "hub", false, true)
	addRegistryCredential(t, app, col, "ghcr.io", "two", false, true)

	s, jobID, cmds := setupRegistryAuthJob(t, app, "alpine:3.22")
	cmd := dispatchAndCapture(t, app, s, jobID, cmds)

	if keys := decodeDockerAuthKeys(t, cmd.RegistryAuthB64); !slices.Equal(keys, []string{"https://index.docker.io/v1/"}) {
		t.Errorf("auth keys = %v, want only the Docker Hub key", keys)
	}
	if len(cmd.InsecureRegistries) != 0 {
		t.Errorf("InsecureRegistries = %v, want empty", cmd.InsecureRegistries)
	}
}

func TestExecuteJobUndecryptableCredentialFailsRun(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	t.Setenv(constants.EnvSecretKey, registryAuthTestSecret)
	col := ensureRegistryCredentialsCollection(t, app)
	addRegistryCredential(t, app, col, "registry.example.com:5000", "not-encrypted", false, false)

	s, jobID, cmds := setupRegistryAuthJob(t, app, "registry.example.com:5000/team/app:1.0")
	ids := s.executeJob(jobID, "manual", "test-user")
	if len(ids) != 1 {
		t.Fatalf("run ids = %v, want one run", ids)
	}

	run := waitForJobRunStatus(t, app, ids[0], "error")
	if output := run.GetString("output"); !strings.Contains(output, "registry auth error") {
		t.Errorf("output = %q, want it to mention the registry auth error", output)
	}
	select {
	case cmd := <-cmds:
		t.Fatalf("expected no dispatch, got run %s", cmd.JobRunID)
	default:
	}
}
