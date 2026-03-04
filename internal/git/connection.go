package git

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestConnection(gitURL string, auth transport.AuthMethod) error {
	remote := gogit.NewRemote(nil, &gogitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{gitURL},
	})
	_, err := remote.List(&gogit.ListOptions{Auth: auth})
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}
	return nil
}
