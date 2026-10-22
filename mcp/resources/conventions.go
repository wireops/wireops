package resources

// composeConventionsURI is a static resource, not a template: it describes
// wireops itself, not one record.
const composeConventionsURI = "wireops://conventions/compose"

// composeConventions is the single copy of the wireops compose conventions.
// It used to be duplicated in scaffold_stack's tool description (2 KB that
// every session paid for, whether or not it scaffolded anything) and in the
// scaffold_new_stack prompt. As a resource it is fetched when it is needed,
// and there is one place to change when a convention changes.
const composeConventions = `# wireops compose conventions

These are optional service-level labels and annotations wireops reads at
render time. They are unrelated to ` + "`x-wireops`" + `, which is a stack-level
top-level key, and never overlap with it.

scaffold_stack's structured input has no field for labels or annotations, so
apply these by hand-editing the YAML it returns, only where they are wanted.

## customization.image.slug — a label

Gives the service a proper icon in the wireops UI instead of a generic one.
The value must match an existing slug in the selfh.st/icons catalog
(https://selfh.st/icons/) — do not invent one.

    services:
      app:
        image: nginx:1.31.4-alpine
        labels:
          - "customization.image.slug=nginx"

## dev.wireops.config.<name> — an annotation, not a label

Mounts a git-tracked config file or directory straight into the container.
Put it under an "annotations:" map, never under "labels:". <name> is an
arbitrary identifier distinguishing multiple such annotations on one service
(letters, digits, - and _; no path separators, no leading dot). The value is
"<repo-relative source>:<absolute in-container target>".

wireops synthesizes the compose top-level ` + "`configs:`" + ` block and each
service's ` + "`configs:`" + ` list from this annotation at render time. Never write
` + "`configs:`" + ` yourself, and never look for a wireops.yaml field for it: there
is none.

    services:
      app:
        image: nginx:1.31.4-alpine
        annotations:
          dev.wireops.config.nginx-conf: "conf/nginx.conf:/etc/nginx/nginx.conf"

Rules, enforced at sync/render time — the stack fails to sync if violated:

- source must exist in the repository being committed to.
- source may be a single file or a whole directory. A directory is expanded
  recursively into one mounted file per entry, preserving its relative layout
  under target (source "conf/" containing "conf/site.conf" and
  "conf/certs/a.pem" mounts to "target/site.conf" and "target/certs/a.pem").
  Files do not have to be listed individually.
- source must stay inside the repository: no absolute path, no "../" escaping
  it, and it must not be or contain a symlink. Both are rejected outright, not
  silently followed.
- each resolved file is capped at 1MB, and every config resolved for one stack
  at 5MB combined. Keep large or binary assets out of this mechanism.
- target must be a clean absolute in-container path ("/etc/nginx/nginx.conf"),
  never relative and never containing "..".
- Because the mount is a compose config (a bind mount of a single file), a
  program that rewrites the file by atomic replace (write to a temporary file,
  then rename over it) does not work against it. Such a file belongs in a
  volume, not here.

## customization.init — a label

Marks a service as a run-to-completion container: a migration, a seed script,
or another one-shot job that is expected to run once and exit.

Without it, wireops treats any service whose container is not "running" as
missing and marks the whole stack degraded — a clean exit looks like a crash.
With it, the service counts as unhealthy only if it exits non-zero or gets
stuck in a restart loop; exiting 0 (or being gone entirely, once Docker
garbage-collects it) counts as healthy.

Only add it to a service genuinely meant to exit on its own. On a long-running
service it would hide a real crash.

    services:
      migrate:
        image: myapp:1.4.0
        command: ["./migrate", "up"]
        restart: "no"
        labels:
          - "customization.init=true"
`
