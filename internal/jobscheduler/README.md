# jobscheduler

The `jobscheduler` package manages the scheduling and dispatching of scheduled jobs defined in Git repositories.

## Design Notes

### Git is the Source of Truth
Job definitions, including `name` and `description`, are always synchronized from their respective `job.yaml` files. The application does not compete with Git—any changes pushed to the repository will automatically be reflected in the database when the job is synced or executed.
