package docker

import (
	"context"
	"os"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type Client struct {
	cli *client.Client
}

func NewClient() (*Client, error) {
	// The client negotiates the API version with the daemon on its own.
	opts := []client.Opt{client.FromEnv}

	if host := os.Getenv("DOCKER_HOST"); host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.New(opts...)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		cli.Close()
		return nil, err
	}

	return &Client{cli: cli}, nil
}

func (c *Client) Raw() *client.Client {
	return c.cli
}

func (c *Client) Close() error {
	return c.cli.Close()
}

// commitSHALabel is the per-service label the renderer injects with the commit
// a revision was rendered from (internal/sync/renderer.go's
// injectVersionMetadata). Every service of a stack carries it, so when a
// commit moves, compose recreates every container.
const commitSHALabel = "dev.wireops.repository.commit_sha"

// GetRunningStackCommits retrieves the distinct dev.wireops.repository.commit_sha
// labels across every container belonging to the given stack ID, in the order
// Docker returned them.
//
// It returns the whole set rather than the first match because a deploy killed
// mid-sequence can leave a stack straddling two revisions: compose recreated
// some services at the new commit and never reached the others. One entry means
// every container agrees; more than one means it does not, and a caller
// deciding what the host is running must not settle that by whichever
// container Docker happened to list first.
func (c *Client) GetRunningStackCommits(ctx context.Context, stackID string) ([]string, error) {
	f := client.Filters{}
	f.Add("label", "dev.wireops.stack_id="+stackID)

	containers, err := c.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true, // check all containers in case they are stopped
		Filters: f,
	})
	if err != nil {
		return nil, err
	}

	return distinctCommitSHAs(containers.Items), nil
}

// distinctCommitSHAs collects each container's commit label once, preserving
// the order they were seen in. Split out from the Docker call so the
// set-building is testable without a daemon.
func distinctCommitSHAs(containers []container.Summary) []string {
	var commits []string
	seen := make(map[string]bool, len(containers))
	for _, cnt := range containers {
		val := cnt.Labels[commitSHALabel]
		if val == "" || seen[val] {
			continue
		}
		seen[val] = true
		commits = append(commits, val)
	}
	return commits
}
