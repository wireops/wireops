package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/api/types/volume"
	dockerclient "github.com/moby/moby/client"
	"github.com/wireops/wireops/internal/protocol"
)

func TestMapDockerPorts(t *testing.T) {
	tests := []struct {
		name string
		in   []container.PortSummary
		want []protocol.PortInfo
	}{
		{
			name: "ipv4 published",
			in:   []container.PortSummary{{IP: netip.MustParseAddr("127.0.0.1"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
			want: []protocol.PortInfo{{ContainerPort: 80, Protocol: "tcp", HostIP: "127.0.0.1", HostPort: 8080}},
		},
		{
			name: "ipv6 localhost published",
			in:   []container.PortSummary{{IP: netip.MustParseAddr("::1"), PrivatePort: 80, PublicPort: 8443, Type: "tcp"}},
			want: []protocol.PortInfo{{ContainerPort: 80, Protocol: "tcp", HostIP: "::1", HostPort: 8443}},
		},
		{
			name: "udp published wildcard",
			in:   []container.PortSummary{{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 53, PublicPort: 53, Type: "udp"}},
			want: []protocol.PortInfo{{ContainerPort: 53, Protocol: "udp", HostIP: "0.0.0.0", HostPort: 53}},
		},
		{
			name: "exposed but not published",
			in:   []container.PortSummary{{PrivatePort: 443, Type: "tcp"}},
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
	sizes := volumeUsageSizes([]volume.Volume{
		{Name: "known", UsageData: &volume.UsageData{Size: 123}},
		{Name: "unknown", UsageData: &volume.UsageData{Size: -1}},
		{Name: "missing"},
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

func TestBuildNetworkInfoIPAM(t *testing.T) {
	n := dockernetwork.Network{
		Name: "myproject_default",
		IPAM: dockernetwork.IPAM{Config: []dockernetwork.IPAMConfig{
			{
				Subnet:     netip.MustParsePrefix("172.20.0.0/16"),
				IPRange:    netip.MustParsePrefix("172.20.10.0/24"),
				Gateway:    netip.MustParseAddr("172.20.0.1"),
				AuxAddress: map[string]netip.Addr{"router": netip.MustParseAddr("172.20.0.2")},
			},
			// A pool the daemon reported without any addresses must map to
			// empty strings, not "invalid Prefix" / "invalid IP".
			{},
		}},
	}

	got := buildNetworkInfo(n, false)
	want := []protocol.NetworkIPAMConfig{
		{
			Subnet:       "172.20.0.0/16",
			Gateway:      "172.20.0.1",
			IPRange:      "172.20.10.0/24",
			AuxAddresses: map[string]string{"router": "172.20.0.2"},
		},
		{},
	}
	if !reflect.DeepEqual(got.IPAMConfigs, want) {
		t.Fatalf("IPAMConfigs = %+v, want %+v", got.IPAMConfigs, want)
	}
	if got.Subnet != "172.20.0.0/16" || got.Gateway != "172.20.0.1" {
		t.Fatalf("Subnet/Gateway = %q/%q, want first pool's", got.Subnet, got.Gateway)
	}
}

// roundTripFunc lets a fake Docker Engine API be expressed as a plain
// function instead of standing up a real listener, mirroring the pattern
// the moby client itself uses in its own tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal fake response body: %v", err)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(buf)),
	}
}

func errorResponse(status int, message string) *http.Response {
	buf, _ := json.Marshal(map[string]string{"message": message})
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(buf)),
	}
}

func newFakeDockerClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *dockerclient.Client {
	t.Helper()
	// A fixed API version skips the client's default version negotiation,
	// which would otherwise send a /_ping the fake handlers don't expect.
	cli, err := dockerclient.New(
		dockerclient.WithHTTPClient(&http.Client{Transport: roundTripFunc(handler)}),
		dockerclient.WithAPIVersion(dockerclient.MaxAPIVersion),
	)
	if err != nil {
		t.Fatalf("build fake docker client: %v", err)
	}
	return cli
}

func TestGetStackBindMounts(t *testing.T) {
	cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/containers/json"):
			return jsonResponse(t, http.StatusOK, []container.Summary{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}}), nil
		case strings.Contains(req.URL.Path, "/containers/c1/json"):
			return jsonResponse(t, http.StatusOK, container.InspectResponse{
				Mounts: []container.MountPoint{
					{Type: mount.TypeBind, Source: "/host/data", Destination: "/data"},
					{Type: mount.TypeVolume, Source: "vol1", Destination: "/vol"},
				},
			}), nil
		case strings.Contains(req.URL.Path, "/containers/c2/json"):
			// Same bind mount as c1 - must be deduplicated across containers.
			return jsonResponse(t, http.StatusOK, container.InspectResponse{
				Mounts: []container.MountPoint{
					{Type: mount.TypeBind, Source: "/host/data", Destination: "/data"},
				},
			}), nil
		case strings.Contains(req.URL.Path, "/containers/c3/json"):
			// A per-container inspection failure must leave already discovered
			// binds intact but make the caller mark the collection partial.
			return errorResponse(http.StatusInternalServerError, "boom"), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}
	})

	got, err := getStackBindMounts(context.Background(), cli, "myproject")
	if err == nil || !strings.Contains(err.Error(), "c3") {
		t.Fatalf("getStackBindMounts error = %v, want partial inspection failure for c3", err)
	}

	want := []protocol.VolumeInfo{{
		Name:        "/host/data",
		DockerName:  "/host/data",
		Driver:      "bind",
		Mountpoint:  "/host/data",
		Scope:       "local",
		Type:        "bind",
		Destination: "/data",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("getStackBindMounts() = %+v, want %+v", got, want)
	}
}

func TestGetStackVolumesUsesDiskUsageSizes(t *testing.T) {
	var dfQuery string
	cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/volumes"):
			return jsonResponse(t, http.StatusOK, volume.ListResponse{Volumes: []volume.Volume{
				{Name: "myproject_data", Driver: "local", Labels: map[string]string{"com.docker.compose.volume": "data"}},
			}}), nil
		case strings.HasSuffix(req.URL.Path, "/system/df"):
			dfQuery = req.URL.RawQuery
			return jsonResponse(t, http.StatusOK, system.DiskUsage{VolumeUsage: &volume.DiskUsage{
				Items: []volume.Volume{{Name: "myproject_data", UsageData: &volume.UsageData{Size: 4096}}},
			}}), nil
		case strings.HasSuffix(req.URL.Path, "/containers/json"):
			return jsonResponse(t, http.StatusOK, []container.Summary{}), nil
		default:
			// A per-volume inspect here would mean the disk-usage sizes were
			// not picked up.
			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}
	})

	got, err := GetStackVolumes(context.Background(), cli, "myproject")
	if err != nil {
		t.Fatalf("GetStackVolumes returned error: %v", err)
	}
	// Without verbose the daemon only returns totals, and every volume would
	// fall back to a per-volume inspect.
	if !strings.Contains(dfQuery, "type=volume") || !strings.Contains(dfQuery, "verbose=1") {
		t.Fatalf("/system/df query = %q, want volume type and verbose", dfQuery)
	}
	if len(got) != 1 || got[0].Name != "data" || got[0].SizeBytes == nil || *got[0].SizeBytes != 4096 {
		t.Fatalf("GetStackVolumes() = %+v, want one volume named data with 4096 bytes", got)
	}
}

func TestResourceDiscoverySkipsObjectsRemovedDuringInspection(t *testing.T) {
	cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/containers/json"):
			return jsonResponse(t, http.StatusOK, []container.Summary{{ID: "gone-bind"}, {ID: "networked"}}), nil
		case strings.Contains(req.URL.Path, "/containers/gone-bind/json"):
			return errorResponse(http.StatusNotFound, "No such container"), nil
		case strings.Contains(req.URL.Path, "/containers/networked/json"):
			return jsonResponse(t, http.StatusOK, container.InspectResponse{NetworkSettings: &container.NetworkSettings{
				Networks: map[string]*dockernetwork.EndpointSettings{"gone-network": {NetworkID: "gone-network"}},
			}}), nil
		case strings.Contains(req.URL.Path, "/networks/gone-network"):
			return errorResponse(http.StatusNotFound, "network not found"), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}
	})

	binds, err := getStackBindMounts(context.Background(), cli, "myproject")
	if err != nil || len(binds) != 0 {
		t.Fatalf("gone bind container = %#v, %v", binds, err)
	}
	networks, err := getExternalStackNetworks(context.Background(), cli, "myproject", map[string]bool{})
	if err != nil || len(networks) != 0 {
		t.Fatalf("gone external network = %#v, %v", networks, err)
	}
}

func TestGetExternalStackNetworks(t *testing.T) {
	cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/containers/json"):
			return jsonResponse(t, http.StatusOK, []container.Summary{{ID: "c1"}, {ID: "c2"}}), nil
		case strings.Contains(req.URL.Path, "/containers/c1/json"):
			return jsonResponse(t, http.StatusOK, container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*dockernetwork.EndpointSettings{
						"myproject_default": {NetworkID: "owned-id"},
						"legacy_ext":        {NetworkID: "ext-id"},
					},
				},
			}), nil
		case strings.Contains(req.URL.Path, "/containers/c2/json"):
			// A second container attached to the same external network
			// under a different local alias - must dedupe by NetworkID,
			// not by name, and must not trigger a second NetworkInspect.
			return jsonResponse(t, http.StatusOK, container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*dockernetwork.EndpointSettings{
						"ext_alias": {NetworkID: "ext-id"},
					},
				},
			}), nil
		case strings.Contains(req.URL.Path, "/networks/ext-id"):
			return jsonResponse(t, http.StatusOK, dockernetwork.Inspect{Network: dockernetwork.Network{
				ID:     "ext-id",
				Name:   "legacy_ext",
				Driver: "bridge",
				Scope:  "local",
			}}), nil
		case strings.Contains(req.URL.Path, "/networks/owned-id"):
			t.Fatal("owned network must not be re-inspected - it was already seen by the caller")
			return nil, nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}
	})

	seenNetworkIDs := map[string]bool{"owned-id": true}
	got, err := getExternalStackNetworks(context.Background(), cli, "myproject", seenNetworkIDs)
	if err != nil {
		t.Fatalf("getExternalStackNetworks returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("getExternalStackNetworks() returned %d networks, want 1: %+v", len(got), got)
	}
	if got[0].ID != "ext-id" || !got[0].External {
		t.Fatalf("getExternalStackNetworks()[0] = %+v, want ID=ext-id External=true", got[0])
	}
}
