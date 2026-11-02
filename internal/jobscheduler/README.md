# jobscheduler

The `jobscheduler` package manages the scheduling and dispatching of scheduled jobs defined in Git repositories.

## Design Notes

### Git is the Source of Truth
Job definitions, including `name` and `description`, are always synchronized from their respective `job.yaml` files. The application does not compete with Git—any changes pushed to the repository will automatically be reflected in the database when the job is synced or executed.

### Registry Credentials
Each dispatch resolves the registry host of the job's `image` (bare names map to `docker.io`) and attaches the matching stored registry credential to the `RunJobCommand`, exactly like stack deploys. The worker uses it through a per-run `DOCKER_CONFIG`, so no `docker login` is needed on the worker host. No matching credential means an anonymous pull; a matching credential that cannot be decrypted fails the run before dispatch.
