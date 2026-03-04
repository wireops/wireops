package sync

import (
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func toTransportAuth(auth interface{}) transport.AuthMethod {
	if auth == nil {
		return nil
	}
	switch v := auth.(type) {
	case *gogitssh.PublicKeys:
		return v
	case *gogithttp.BasicAuth:
		return v
	}
	return nil
}

func mustParseHash(sha string) plumbing.Hash {
	return plumbing.NewHash(sha)
}
