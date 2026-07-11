package policy_test

import (
	"encoding/json"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/wireops/wireops/internal/policy"
)

func TestValidateComposeConfig(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	// Worker Policies
	policies := core.NewBaseCollection("worker_policies")
	policies.Fields.Add(&core.BoolField{Name: "enabled"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_volumes"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_networks"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_images"})
	policies.Fields.Add(&core.BoolField{Name: "prevent_latest_images"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_volumes"})
	if err := app.Save(policies); err != nil {
		t.Fatalf("failed to create worker_policies: %v", err)
	}

	// Set global policy: block latest images, allow only nginx:* and redis:* images.
	globalPolicy := core.NewRecord(policies)
	globalPolicy.Set("enabled", true)
	globalPolicy.Set("prevent_latest_images", true)
	globalPolicy.Set("allowed_images", `["nginx:*", "redis:*"]`)
	globalPolicy.Set("allowed_volumes", `["/opt/data"]`)
	globalPolicy.Set("allowed_networks", `["default_net"]`)
	if err := app.Save(globalPolicy); err != nil {
		t.Fatalf("failed to save global policy: %v", err)
	}

	wp, err := policy.Load(app, "")
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "valid stack",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.23",
						"volumes": [
							{"type": "bind", "source": "/opt/data/web"}
						]
					}
				}
			}`,
			wantErr: false,
		},
		{
			name: "invalid image tag latest",
			config: `{
				"services": {
					"web": {
						"image": "nginx:latest"
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "invalid image pattern",
			config: `{
				"services": {
					"db": {
						"image": "mysql:8"
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "invalid volume",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.23",
						"volumes": [
							{"type": "bind", "source": "/etc/shadow"}
						]
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "invalid short-syntax volume",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.23",
						"volumes": [
							"- \"/etc/shadow:/etc/shadow\""
						]
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "invalid standard short-syntax volume",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.23",
						"volumes": [
							"/etc/shadow:/etc/shadow"
						]
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "invalid network",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.23",
						"networks": [
							"restricted_net"
						]
					}
				}
			}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var configMap map[string]interface{}
			if err := json.Unmarshal([]byte(tc.config), &configMap); err != nil {
				t.Fatalf("failed to unmarshal config: %v", err)
			}
			err := wp.ValidateComposeConfig(configMap)
			if (err != nil) != tc.wantErr {
				t.Errorf("expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateComposeConfigHardenedOptions(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	policies := core.NewBaseCollection("worker_policies")
	policies.Fields.Add(&core.BoolField{Name: "enabled"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_volumes"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_networks"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_images"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_cap_add"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_devices"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_security_opt"})
	policies.Fields.Add(&core.BoolField{Name: "prevent_latest_images"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_volumes"})
	policies.Fields.Add(&core.BoolField{Name: "block_privileged"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_network"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_pid"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_ipc"})
	policies.Fields.Add(&core.BoolField{Name: "block_docker_socket"})
	if err := app.Save(policies); err != nil {
		t.Fatalf("failed to create worker_policies: %v", err)
	}

	globalPolicy := core.NewRecord(policies)
	globalPolicy.Set("enabled", true)
	globalPolicy.Set("block_privileged", true)
	globalPolicy.Set("block_host_network", true)
	globalPolicy.Set("block_host_pid", true)
	globalPolicy.Set("block_host_ipc", true)
	globalPolicy.Set("block_docker_socket", true)
	globalPolicy.Set("allowed_cap_add", `["NET_ADMIN"]`)
	globalPolicy.Set("allowed_devices", `["/dev/ttyUSB0"]`)
	globalPolicy.Set("allowed_security_opt", `["no-new-privileges:true"]`)
	if err := app.Save(globalPolicy); err != nil {
		t.Fatalf("failed to save global policy: %v", err)
	}

	wp, err := policy.Load(app, "")
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "all clear",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"cap_add": ["NET_ADMIN"],
						"devices": ["/dev/ttyUSB0:/dev/ttyUSB0"],
						"security_opt": ["no-new-privileges:true"]
					}
				}
			}`,
			wantErr: false,
		},
		{
			name: "privileged blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"privileged": true
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "network_mode host blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"network_mode": "host"
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "pid host blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"pid": "host"
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "ipc host blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"ipc": "host"
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "docker socket via volume blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"volumes": [
							"/var/run/docker.sock:/var/run/docker.sock"
						]
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "docker socket via device blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"devices": [
							"/var/run/docker.sock:/var/run/docker.sock"
						]
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "docker socket via named volume blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"volumes": [
							"docker-sock:/var/run/docker.sock"
						]
					}
				},
				"volumes": {
					"docker-sock": {
						"driver_opts": {
							"type": "none",
							"o": "bind",
							"device": "/var/run/docker.sock"
						}
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "cap_add not in allowlist blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"cap_add": ["SYS_ADMIN"]
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "device not in allowlist blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"devices": ["/dev/sda:/dev/sda"]
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "security_opt not in allowlist blocked",
			config: `{
				"services": {
					"web": {
						"image": "nginx:1.25",
						"security_opt": ["seccomp:unconfined"]
					}
				}
			}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var configMap map[string]interface{}
			if err := json.Unmarshal([]byte(tc.config), &configMap); err != nil {
				t.Fatalf("failed to unmarshal config: %v", err)
			}
			err := wp.ValidateComposeConfig(configMap)
			if (err != nil) != tc.wantErr {
				t.Errorf("expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}
