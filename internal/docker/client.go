package docker

import (
	"context"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type Client struct {
	cli *client.Client
}

func NewClient() (*Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}

	if host := os.Getenv("DOCKER_HOST"); host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if _, err := cli.Ping(ctx); err != nil {
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

// GetRunningStackCommit retrieves the dev.wireops.repository.commit_sha label from a running container
// belonging to the given stack ID.
func (c *Client) GetRunningStackCommit(ctx context.Context, stackID string) (string, error) {
	f := filters.NewArgs()
	f.Add("label", "dev.wireops.stack_id="+stackID)

	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true, // check all containers in case they are stopped
		Filters: f,
	})
	if err != nil {
		return "", err
	}

	for _, cnt := range containers {
		if val, ok := cnt.Labels["dev.wireops.repository.commit_sha"]; ok && val != "" {
			return val, nil
		}
	}
	return "", nil
}
