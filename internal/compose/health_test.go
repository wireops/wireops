package compose

import "testing"

func TestClassifyServiceInstances(t *testing.T) {
	cases := []struct {
		name      string
		instances []ServiceStatus
		isInit    bool
		strict    bool
		want      ServiceHealthState
	}{
		{name: "missing long-running service", want: ServiceMissing},
		{name: "clean completed init may be absent", isInit: true, want: ServiceHealthy},
		{name: "clean completed init", isInit: true, instances: []ServiceStatus{{Status: "exited", ExitCode: 0}}, want: ServiceHealthy},
		{name: "failed init", isInit: true, instances: []ServiceStatus{{Status: "exited", ExitCode: 1}}, want: ServiceUnhealthy},
		{name: "starting service", instances: []ServiceStatus{{Status: "created"}}, strict: true, want: ServiceStarting},
		{name: "strict health rejects one bad replica", instances: []ServiceStatus{{Status: "running", Health: "healthy"}, {Status: "running", Health: "unhealthy"}}, strict: true, want: ServiceUnhealthy},
		{name: "post-check remains compatible with one good replica", instances: []ServiceStatus{{Status: "running", Health: "healthy"}, {Status: "running", Health: "unhealthy"}}, want: ServiceHealthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyServiceInstances(tc.instances, tc.isInit, tc.strict); got != tc.want {
				t.Fatalf("ClassifyServiceInstances() = %q, want %q", got, tc.want)
			}
		})
	}
}
