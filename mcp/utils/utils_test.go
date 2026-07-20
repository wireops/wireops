package utils

import (
	"reflect"
	"testing"
)

func TestToInterfaceSlice(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []interface{}
	}{
		{"empty", []string{}, []interface{}{}},
		{"nil", nil, []interface{}{}},
		{"single", []string{"a"}, []interface{}{"a"}},
		{"multiple", []string{"a", "b", "c"}, []interface{}{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToInterfaceSlice(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToInterfaceSlice(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestVolumeSource(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want string
	}{
		{"named volume with target", "myvolume:/data", "myvolume"},
		{"absolute bind mount", "/host/path:/container/path", "/host/path"},
		{"absolute bind mount with access mode", "/host/path:/container/path:ro", "/host/path"},
		{"relative bind mount ./", "./data:/data", "./data"},
		{"relative bind mount ../", "../data:/data", "../data"},
		{"home-relative bind mount", "~/data:/data", "~/data"},
		{"bare absolute path no target", "/absolute/path", "/absolute/path"},
		{"bare relative path no target", "./relative/path", "./relative/path"},
		{"bare named volume no target", "myvolume", ""},
		{"empty input", "", ""},
		{"whitespace only", "   ", ""},
		{"windows drive letter path", `C:\Users\foo\data:/data`, `C:\Users\foo\data`},
		{"windows drive letter no target", `C:\Users\foo\data`, `C:\Users\foo\data`},
		{"windows drive letter forward slash", `C:/data:/app`, `C:/data`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VolumeSource(tt.spec)
			if got != tt.want {
				t.Errorf("VolumeSource(%q) = %q, want %q", tt.spec, got, tt.want)
			}
		})
	}
}

func TestIsHostPath(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"absolute path", "/host/path", true},
		{"relative ./ path", "./data", true},
		{"relative ../ path", "../data", true},
		{"home ~ path", "~/data", true},
		{"named volume", "myvolume", false},
		{"empty string", "", false},
		{"windows drive letter path", `C:\Users\foo\data`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsHostPath(tt.src)
			if got != tt.want {
				t.Errorf("IsHostPath(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}
