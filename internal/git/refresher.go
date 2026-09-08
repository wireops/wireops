package git

import (
	"context"
	"log"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// RefreshExpiringOAuthTokens proactively refreshes every oauth_token
// repository_keys row whose access token is within refreshGraceWindow of
// expiring (or already past it). Without this, a GitLab-backed repository
// key that sits idle for a couple hours between syncs never exercises the
// lazy refresh in credential_store.go, and the refresh token — which
// GitLab rotates on use and eventually stops honoring past expiry — goes
// stale with nothing to redeem it in time. LoadOAuthToken already no-ops
// for a token that isn't due yet, so calling it here for every connected
// credential on a cron tick is cheap and safe to run frequently; it also
// shares refreshGroup with any concurrent lazy refresh triggered by a sync
// or browse request, so the two paths can't race to redeem the same
// refresh_token.
func RefreshExpiringOAuthTokens(ctx context.Context, app core.App) {
	records, err := app.FindAllRecords("repository_keys", dbx.HashExp{"auth_type": string(AuthTypeOAuthToken)})
	if err != nil {
		log.Printf("[git] oauth token refresh sweep: list repository_keys: %v", err)
		return
	}

	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		if _, _, err := LoadOAuthToken(ctx, app, record.Id); err != nil {
			log.Printf("[git] oauth token refresh sweep: key %s: %v", record.Id, err)
		}
	}
}
