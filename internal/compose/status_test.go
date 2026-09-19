package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
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

// roundTripFunc lets a fake Docker Engine API be expressed as a plain
// function instead of standing up a real listener, mirroring the pattern
// the docker/docker client itself uses in its own tests.
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
	cli, err := dockerclient.NewClientWithOpts(dockerclient.WithHTTPClient(&http.Client{Transport: roundTripFunc(handler)}))
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
			// Inspect failure must be skipped rather than aborting the scan.
			return errorResponse(http.StatusInternalServerError, "boom"), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}
	})

	got, err := getStackBindMounts(context.Background(), cli, "myproject")
	if err != nil {
		t.Fatalf("getStackBindMounts returned error: %v", err)
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
						"legacy_ext":         {NetworkID: "ext-id"},
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
			return jsonResponse(t, http.StatusOK, dockernetwork.Inspect{
				ID:     "ext-id",
				Name:   "legacy_ext",
				Driver: "bridge",
				Scope:  "local",
			}), nil
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
