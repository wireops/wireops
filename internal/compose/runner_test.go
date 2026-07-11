package compose

import (
	"reflect"
	"testing"
)

func TestBuildUpArgs(t *testing.T) {
	cases := []struct {
		name          string
		composeFile   string
		removeOrphans bool
		forcePull     bool
		want          []string
	}{
		{
			name:        "NoFlags",
			composeFile: "docker-compose.yml",
			want:        []string{"compose", "-f", "docker-compose.yml", "up", "-d"},
		},
		{
			name:          "RemoveOrphansOnly",
			composeFile:   "docker-compose.yml",
			removeOrphans: true,
			want:          []string{"compose", "-f", "docker-compose.yml", "up", "-d", "--remove-orphans"},
		},
		{
			name:        "ForcePullOnly",
			composeFile: "docker-compose.yml",
			forcePull:   true,
			want:        []string{"compose", "-f", "docker-compose.yml", "up", "-d", "--pull", "always"},
		},
		{
			name:          "BothFlags",
			composeFile:   "compose.yml",
			removeOrphans: true,
			forcePull:     true,
			want:          []string{"compose", "-f", "compose.yml", "up", "-d", "--remove-orphans", "--pull", "always"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildUpArgs(tc.composeFile, tc.removeOrphans, tc.forcePull)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildUpArgs(%q, %v, %v) = %v, want %v", tc.composeFile, tc.removeOrphans, tc.forcePull, got, tc.want)
			}
		})
	}
}
