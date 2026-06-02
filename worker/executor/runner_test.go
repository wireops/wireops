package executor

import (
	"os"
	"testing"

	"github.com/wireops/wireops/internal/protocol"
)

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		val     string
		pattern string
		want    bool
	}{
		{"postgres:14", "postgres:*", true},
		{"postgres:14", "mysql:*", false},
		{"ubuntu:latest", "*:latest", true},
		{"ubuntu:latest", "*", true},
		{"ubuntu:latest", "ubuntu:latest", true},
		{"my-registry.com/ubuntu:latest", "my-registry.com/*", true},
		{"my-registry.com/ubuntu:latest", "*ubuntu:*", true},
		{"my-registry.com/ubuntu:14.04", "*ubuntu:14*", true},
		{"my-registry.com/ubuntu:14.04", "*ubuntu:15*", false},
	}

	for _, tc := range cases {
		got := matchPattern(tc.val, tc.pattern)
		if got != tc.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tc.val, tc.pattern, got, tc.want)
		}
	}
}

func TestValidateImage(t *testing.T) {
	// 1. Empty allowed images (allow all)
	if err := validateImage("ubuntu:latest", ""); err != nil {
		t.Errorf("expected no error when allowed string is empty, got: %v", err)
	}

	// 2. Matching image
	if err := validateImage("postgres:14", "postgres:*,mysql:*"); err != nil {
		t.Errorf("expected no error for matching image, got: %v", err)
	}

	// 3. Unmatching image
	err := validateImage("redis:alpine", "postgres:*,mysql:*")
	if err == nil {
		t.Error("expected error for non-matching image, got nil")
	}

	// 4. Empty image
	if err := validateImage("", "postgres:*"); err == nil {
		t.Error("expected error for empty image, got nil")
	}
}

func TestValidateNetwork(t *testing.T) {
	// 1. Empty allowed networks (allow all)
	if err := validateNetwork("bridge", ""); err != nil {
		t.Errorf("expected no error when allowed networks is empty, got: %v", err)
	}

	// 2. Matching network
	if err := validateNetwork("frontend", "frontend,backend"); err != nil {
		t.Errorf("expected no error for matching network, got: %v", err)
	}

	// 3. Unmatching network
	if err := validateNetwork("host", "frontend,backend"); err == nil {
		t.Error("expected error for non-matching network, got nil")
	}

	// 4. Default network when empty network is passed
	if err := validateNetwork("", "default"); err != nil {
		t.Errorf("expected default network to match allowed 'default' pattern, got: %v", err)
	}
}

func TestValidateVolumes(t *testing.T) {
	// 1. Empty allowed volumes (should allow non-system paths, reject forbidden paths)
	if err := validateVolumes([]string{"/home/user/data:/data", "db_val:/var/lib/mysql"}, ""); err != nil {
		t.Errorf("expected no error for safe paths when allowed string is empty, got: %v", err)
	}

	// 2. Reject forbidden path /etc
	if err := validateVolumes([]string{"/etc:/etc"}, ""); err == nil {
		t.Error("expected error for mounting /etc, got nil")
	}

	// 3. Reject forbidden path /var/run/docker.sock
	if err := validateVolumes([]string{"/var/run/docker.sock:/var/run/docker.sock"}, ""); err == nil {
		t.Error("expected error for mounting docker socket, got nil")
	}

	// 4. Reject traversal path /etc/../etc/passwd or similar
	if err := validateVolumes([]string{"/etc/../etc:/etc"}, ""); err == nil {
		t.Error("expected error for mounting traverse to /etc, got nil")
	}

	// 5. Allowed volumes restriction configured: match pattern
	if err := validateVolumes([]string{"/mnt/data/job1:/data"}, "/mnt/data/*"); err != nil {
		t.Errorf("expected no error for allowed pattern /mnt/data/*, got: %v", err)
	}

	// 6. Allowed volumes restriction configured: unmatching pattern
	if err := validateVolumes([]string{"/home/user/data:/data"}, "/mnt/data/*"); err == nil {
		t.Error("expected error for non-matching path under /mnt/data/* restrict, got nil")
	}

	// 7. Named volumes with allowed restriction
	if err := validateVolumes([]string{"my_named_volume:/data"}, "my_named_volume"); err != nil {
		t.Errorf("expected no error for matching named volume, got: %v", err)
	}

	if err := validateVolumes([]string{"other_named_volume:/data"}, "my_named_volume"); err == nil {
		t.Error("expected error for non-matching named volume when restriction is configured, got nil")
	}
}

func TestValidateJobSecurity(t *testing.T) {
	os.Setenv("WORKER_ALLOWED_IMAGES", "alpine:*,postgres:*")
	os.Setenv("WORKER_ALLOWED_NETWORKS", "bridge,host")
	os.Setenv("WORKER_ALLOWED_VOLUMES", "/mnt/data/*,named_vol")
	defer func() {
		os.Unsetenv("WORKER_ALLOWED_IMAGES")
		os.Unsetenv("WORKER_ALLOWED_NETWORKS")
		os.Unsetenv("WORKER_ALLOWED_VOLUMES")
	}()

	// 1. Valid command
	cmdValid := protocol.RunJobCommand{
		Image:   "alpine:latest",
		Network: "bridge",
		Volumes: []string{"/mnt/data/1:/data", "named_vol:/var/lib/mysql"},
	}
	if err := validateJobSecurity(cmdValid); err != nil {
		t.Errorf("expected valid command to pass security, got: %v", err)
	}

	// 2. Invalid image
	cmdInvalidImage := cmdValid
	cmdInvalidImage.Image = "ubuntu:latest"
	if err := validateJobSecurity(cmdInvalidImage); err == nil {
		t.Error("expected invalid image to fail security, got nil")
	}

	// 3. Invalid network
	cmdInvalidNet := cmdValid
	cmdInvalidNet.Network = "custom_net"
	if err := validateJobSecurity(cmdInvalidNet); err == nil {
		t.Error("expected invalid network to fail security, got nil")
	}

	// 4. Invalid volume mount
	cmdInvalidVol := cmdValid
	cmdInvalidVol.Volumes = []string{"/home/user:/data"}
	if err := validateJobSecurity(cmdInvalidVol); err == nil {
		t.Error("expected invalid volume to fail security, got nil")
	}
}
