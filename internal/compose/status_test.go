package compose

import (
	"reflect"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/volume"
	"github.com/wireops/wireops/internal/protocol"
)

func TestMapDockerPorts(t *testing.T) {
	tests := []struct {
		name string
		in   []container.Port
		want []protocol.PortInfo
	}{
		{
			name: "ipv4 published",
			in:   []container.Port{{IP: "127.0.0.1", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
			want: []protocol.PortInfo{{ContainerPort: 80, Protocol: "tcp", HostIP: "127.0.0.1", HostPort: 8080}},
		},
		{
			name: "ipv6 localhost published",
			in:   []container.Port{{IP: "::1", PrivatePort: 80, PublicPort: 8443, Type: "tcp"}},
			want: []protocol.PortInfo{{ContainerPort: 80, Protocol: "tcp", HostIP: "::1", HostPort: 8443}},
		},
		{
			name: "udp published wildcard",
			in:   []container.Port{{IP: "0.0.0.0", PrivatePort: 53, PublicPort: 53, Type: "udp"}},
			want: []protocol.PortInfo{{ContainerPort: 53, Protocol: "udp", HostIP: "0.0.0.0", HostPort: 53}},
		},
		{
			name: "exposed but not published",
			in:   []container.Port{{PrivatePort: 443, Type: "tcp"}},
			want: []protocol.PortInfo{{ContainerPort: 443, Protocol: "tcp"}},
		},
		{
			name: "no ports",
			in:   nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapDockerPorts(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("mapDockerPorts(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestVolumeUsageSizes(t *testing.T) {
	sizes := volumeUsageSizes([]*volume.Volume{
		{Name: "known", UsageData: &volume.UsageData{Size: 123}},
		{Name: "unknown", UsageData: &volume.UsageData{Size: -1}},
		{Name: "missing"},
		nil,
	})

	if got := sizes["known"]; got != 123 {
		t.Fatalf("size for known volume = %d, want 123", got)
	}
	if _, ok := sizes["unknown"]; ok {
		t.Fatal("unknown size must not be included")
	}
	if _, ok := sizes["missing"]; ok {
		t.Fatal("missing size must not be included")
	}
}
