package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

func summary(commitSHA string) container.Summary {
	labels := map[string]string{"dev.wireops.stack_id": "stack-1"}
	if commitSHA != "" {
		labels[commitSHALabel] = commitSHA
	}
	return container.Summary{Labels: labels}
}

// TestDistinctCommitSHAs pins the behaviour the timeout convergence in
// internal/sync depends on: the whole set, not the first match. A
// `docker compose up` killed mid-sequence recreates some services at the new
// commit and never reaches the others, so the containers disagree — and
// stopping at the first label would let whichever container Docker listed
// first decide what the server records for the whole stack.
func TestDistinctCommitSHAs(t *testing.T) {
	cases := []struct {
		name       string
		containers []container.Summary
		want       []string
	}{
		{
			name: "NoContainers",
			want: nil,
		},
		{
			name:       "EveryContainerAgrees",
			containers: []container.Summary{summary("commit-a"), summary("commit-a")},
			want:       []string{"commit-a"},
		},
		{
			name:       "DoesNotStopAtTheFirstLabel",
			containers: []container.Summary{summary("commit-a"), summary("commit-b")},
			want:       []string{"commit-a", "commit-b"},
		},
		{
			name:       "PreservesTheOrderSeen",
			containers: []container.Summary{summary("commit-b"), summary("commit-a")},
			want:       []string{"commit-b", "commit-a"},
		},
		{
			name:       "SkipsUnlabeledContainers",
			containers: []container.Summary{summary(""), summary("commit-a"), summary("")},
			want:       []string{"commit-a"},
		},
		{
			name:       "UnlabeledOnly",
			containers: []container.Summary{summary(""), summary("")},
			want:       nil,
		},
		{
			name: "DeduplicatesAcrossReplicas",
			containers: []container.Summary{
				summary("commit-a"), summary("commit-b"), summary("commit-a"), summary("commit-b"),
			},
			want: []string{"commit-a", "commit-b"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := distinctCommitSHAs(tc.containers)
			if len(got) != len(tc.want) {
				t.Fatalf("distinctCommitSHAs = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("distinctCommitSHAs = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
