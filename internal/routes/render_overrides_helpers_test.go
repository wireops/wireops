package routes

import (
	"reflect"
	"testing"
)

func TestComposePortsToShortForm(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
		want []string
	}{
		{
			name: "string short-form entries passed through",
			raw:  []interface{}{"8080:80", "9090:90/udp"},
			want: []string{"8080:80", "9090:90/udp"},
		},
		{
			name: "object with tcp protocol omits suffix",
			raw: []interface{}{
				map[string]interface{}{"published": "8080", "target": float64(80), "protocol": "tcp"},
			},
			want: []string{"8080:80"},
		},
		{
			name: "object with non-tcp protocol keeps suffix",
			raw: []interface{}{
				map[string]interface{}{"published": "8080", "target": float64(80), "protocol": "udp"},
			},
			want: []string{"8080:80/udp"},
		},
		{
			name: "object with no protocol field omits suffix",
			raw: []interface{}{
				map[string]interface{}{"published": "8080", "target": float64(80)},
			},
			want: []string{"8080:80"},
		},
		{
			name: "float64 published and target formatted as plain integers",
			raw: []interface{}{
				map[string]interface{}{"published": float64(8080), "target": float64(80)},
			},
			want: []string{"8080:80"},
		},
		{
			name: "missing published is skipped",
			raw: []interface{}{
				map[string]interface{}{"target": float64(80)},
			},
			want: nil,
		},
		{
			name: "missing target is skipped (target-only port)",
			raw: []interface{}{
				map[string]interface{}{"published": "8080"},
			},
			want: nil,
		},
		{
			name: "mixed string and object entries",
			raw: []interface{}{
				"1234:1234",
				map[string]interface{}{"published": "8080", "target": float64(80)},
			},
			want: []string{"1234:1234", "8080:80"},
		},
		{
			name: "unsupported entry type is ignored",
			raw:  []interface{}{42},
			want: nil,
		},
		{
			name: "not a list returns nil",
			raw:  map[string]interface{}{"published": "8080"},
			want: nil,
		},
		{
			name: "nil input returns nil",
			raw:  nil,
			want: nil,
		},
		{
			name: "empty list returns nil",
			raw:  []interface{}{},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := composePortsToShortForm(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("composePortsToShortForm(%#v) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestPortNumberString(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
		want string
	}{
		{name: "float64 formatted as plain integer", raw: float64(8080), want: "8080"},
		{name: "large float64 avoids scientific notation", raw: float64(1000000), want: "1000000"},
		{name: "string passed through unchanged", raw: "8080", want: "8080"},
		{name: "nil returns empty string", raw: nil, want: ""},
		{name: "unsupported type returns empty string", raw: true, want: ""},
		{name: "int type unsupported, returns empty string", raw: 8080, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := portNumberString(tc.raw)
			if got != tc.want {
				t.Errorf("portNumberString(%#v) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestComposeNetworksToList(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
		want []string
	}{
		{
			name: "map form sorted by name",
			raw: map[string]interface{}{
				"proxy":   map[string]interface{}{},
				"backend": nil,
			},
			want: []string{"backend", "proxy"},
		},
		{
			name: "list form preserves order",
			raw:  []interface{}{"proxy", "backend"},
			want: []string{"proxy", "backend"},
		},
		{
			name: "list form skips non-string entries",
			raw:  []interface{}{"proxy", 42, "backend"},
			want: []string{"proxy", "backend"},
		},
		{
			name: "empty map returns empty non-nil slice",
			raw:  map[string]interface{}{},
			want: []string{},
		},
		{
			name: "empty list returns nil",
			raw:  []interface{}{},
			want: nil,
		},
		{
			name: "invalid type returns nil",
			raw:  "not-a-map-or-list",
			want: nil,
		},
		{
			name: "nil input returns nil",
			raw:  nil,
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := composeNetworksToList(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("composeNetworksToList(%#v) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}
