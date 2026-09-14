package sync

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/protocol"
)

type projectIdentity struct {
	TargetName      string
	PreviousName    string
	PreviousCompose []byte
	NeedsMigration  bool
}

func workerHasCapability(worker *core.Record, capability string) bool {
	var capabilities []string
	if err := worker.UnmarshalJSONField("capabilities", &capabilities); err != nil {
		return false
	}
	for _, candidate := range capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

func (r *Reconciler) deployCommandForRender(workerID string, base protocol.DeployCommand, render *RenderResult, recreateContainers, recreateVolumes, recreateNetworks bool) (interface{}, error) {
	needsMigration := render.PreviousProjectName != "" && render.PreviousProjectName != render.ProjectName
	if !needsMigration && !recreateContainers && !recreateVolumes && !recreateNetworks {
		return base, nil
	}

	cmd := protocol.RedeployCommand{
		DeployCommand:      base,
		RecreateContainers: recreateContainers,
		RecreateVolumes:    recreateVolumes,
		RecreateNetworks:   recreateNetworks,
	}
	if !needsMigration {
		return cmd, nil
	}

	worker, err := r.app.FindRecordById("workers", workerID)
	if err != nil {
		return nil, fmt.Errorf("failed to load worker capabilities: %w", err)
	}
	if !workerHasCapability(worker, protocol.CapabilityProjectIdentityMigrationV1) {
		return nil, fmt.Errorf("worker %q must be updated before Compose project identity can migrate from %q to %q", worker.GetString("hostname"), render.PreviousProjectName, render.ProjectName)
	}
	cmd.RecreateContainers = true
	cmd.ProjectMigration = &protocol.ProjectMigrationSpec{
		PreviousProjectName: render.PreviousProjectName,
		TargetProjectName:   render.ProjectName,
		PreviousComposeB64:  base64.StdEncoding.EncodeToString(render.PreviousCompose),
	}
	return cmd, nil
}

func persistProjectIdentity(stack *core.Record, render *RenderResult) {
	if render != nil && render.ProjectName != "" {
		stack.Set("compose_project_name", render.ProjectName)
	}
}

func (r *Renderer) resolveProjectIdentity(stack *core.Record) (projectIdentity, error) {
	target := stack.GetString("compose_project_name")
	if target == "" {
		target = compose.SanitizeProjectName(stack.GetString("name"))
	}
	identity := projectIdentity{TargetName: target}

	version := stack.GetInt("deployed_version")
	if version == 0 {
		// current_version advances as soon as a revision is rendered, including
		// failed deploys, so it must never be used as the migration baseline.
		// A stack with evidence of an older deployment but no deployed_version
		// (e.g. deployed before migration 41 introduced the field) has no
		// trustworthy baseline. Rather than permanently blocking every future
		// reconcile — GenerateRevision would fail forever, never able to set
		// deployed_version — resolve to the target identity with migration
		// disabled. This is non-destructive: the renderer tears nothing down,
		// a plain deploy runs under the target name, and deployed_version is
		// backfilled on the next success so later reconciles behave normally.
		return identity, nil
	}

	content, err := os.ReadFile(r.GetRevisionFilePath(stack.Id, version))
	if err != nil {
		return projectIdentity{}, fmt.Errorf("cannot safely resolve deployed Compose identity from revision v%d: %w", version, err)
	}
	previousName, err := compose.ExtractProjectName(content)
	if err != nil {
		// Revisions rendered before the top-level `name` field was guaranteed
		// present have no baseline to migrate from. Fall back to the target
		// identity with migration disabled rather than failing the render.
		return identity, nil
	}
	identity.PreviousName = previousName
	identity.PreviousCompose = content
	identity.NeedsMigration = previousName != target
	return identity, nil
}

// DeployedProjectName is the authoritative identity for runtime reads and
// container actions. During a failed/in-flight migration it deliberately
// continues to resolve the last successful revision rather than current_version.
func DeployedProjectName(stack *core.Record) (string, error) {
	if name := stack.GetString("compose_project_name"); name != "" {
		return name, nil
	}
	r := NewRenderer(nil)
	identity, err := r.resolveProjectIdentity(stack)
	if err != nil {
		return "", err
	}
	if identity.PreviousName != "" {
		return identity.PreviousName, nil
	}
	return identity.TargetName, nil
}
