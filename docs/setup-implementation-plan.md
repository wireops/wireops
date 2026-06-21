# Setup Implementation Plan

## Objective

Improve the first-run setup experience for Wireops with small, low-risk iterations that reduce frontend fragility, improve user guidance, and make the bootstrap flow easier to maintain.

## Scope

This plan focuses on:

- the initial instance status check
- first administrator creation
- the `/setup` frontend experience
- bootstrap maintainability and observability

Note: during PR 1, Wireops introduced `BOOTSTRAP_TOKEN` as an explicit first-run requirement. The plan below reflects that decision and treats the bootstrap token as the primary setup gate instead of localhost-only access.

This plan does not include broader authentication redesign, installer wizard expansion, or worker onboarding flows.

## Recommended Delivery

Ship in 3 small pull requests:

1. setup status hardening
2. setup UX improvements
3. bootstrap maintainability and observability

## PR 1: Setup Status Hardening

### Goal

Return enough setup context from the backend so the frontend can make explicit routing and messaging decisions without fragile heuristics.

### Backend

Expand `GET /api/custom/setup/status` in `/Users/jfxdev/Documents/workspace/apps/wireops/internal/routes/setup.go` to return:

- `needsSetup`
- `setupAllowed`
- `reason`
- `requiresBootstrapToken`

Suggested `reason` values:

- `already_configured`
- `missing_bootstrap_token`
- `unknown`

### Notes

- Reuse the same bootstrap-token-aware decision path used by `POST /api/custom/setup`.
- Keep the endpoint lightweight and safe to call often.
- Preserve backward compatibility only if needed during rollout; otherwise update the frontend in the same PR.

### Tests

Add or update tests for:

- empty instance with bootstrap token configured
- empty instance without bootstrap token configured
- already configured instance
- transient failure path if status cannot be determined

### Expected Outcome

- simpler frontend logic
- clearer setup-state decisions
- lower risk of redirect regressions

## PR 2: Setup UX Improvements

### Goal

Make the first-run experience clearer and more self-explanatory without changing the core auth model.

With `BOOTSTRAP_TOKEN` now required, the UX should help operators understand why setup may be blocked and what value is required to finish first-run bootstrap.

### Frontend

Update `/Users/jfxdev/Documents/workspace/apps/wireops/frontend/app/pages/setup.vue` to:

- load and react to the expanded setup status
- show a clear message when setup is blocked because `BOOTSTRAP_TOKEN` is missing or setup is already complete
- explain what the bootstrap token is for before submit
- explain password requirements before submit
- map known backend errors to friendly UI messages
- show an explicit fallback if automatic login fails after account creation

Possible messages to support:

- setup already completed
- bootstrap token missing on the server
- invalid bootstrap token
- invalid email
- password too short
- password mismatch
- unexpected internal error

### Middleware

Keep `/Users/jfxdev/Documents/workspace/apps/wireops/frontend/app/middleware/auth.global.ts` conservative:

- use setup status for routing
- avoid broad redirect logic during initial route resolution
- prefer explicit state handling over implicit assumptions
- route fresh unauthenticated instances toward `/setup`, while leaving blocked-state explanation to the setup page itself

### Expected Outcome

- clearer onboarding
- fewer support questions around bootstrap-token-based setup
- less confusion after setup submission failures

## PR 3: Bootstrap Maintainability And Observability

### Goal

Reduce long-term risk in the setup flow by centralizing first-admin creation and improving visibility into failures.

### Backend Refactor

Extract the first-admin creation flow from `/Users/jfxdev/Documents/workspace/apps/wireops/internal/routes/setup.go` into a dedicated service-level function, for example:

- `CreateInitialAdmin(...)`

This function should centralize:

- the transaction boundary
- `users` creation
- `_superusers` creation
- initial role assignment
- `verified` and `protected` defaults

### Observability

Add structured logs or audit entries for:

- setup started
- setup completed
- setup rejected
- setup failed

### Tests

Add coverage for:

- concurrent first-admin attempts
- transaction failure during bootstrap
- divergence prevention between `users` and `_superusers`

### Expected Outcome

- easier maintenance
- safer future auth changes
- better troubleshooting in development and production

## Suggested Order

1. PR 1 first because it reduces ambiguity in the current flow.
2. PR 2 next because it improves visible user experience with minimal backend churn.
3. PR 3 last because it is mostly internal hardening after behavior is stabilized.

## Risks To Watch

- redirect regressions in global middleware
- setup status requests delaying route resolution
- inconsistent state between `users` and `_superusers`
- confusing behavior when operators do not realize `BOOTSTRAP_TOKEN` must be configured and shared out-of-band

## Success Criteria

- a fresh instance consistently lands on `/setup`
- a configured instance never exposes setup as the primary path
- blocked setup attempts explain whether setup is already complete or waiting on `BOOTSTRAP_TOKEN`
- first-admin creation remains atomic
- setup-related failures are diagnosable from logs and tests
