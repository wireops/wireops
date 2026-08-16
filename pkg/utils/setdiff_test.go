package utils

import "testing"

func TestSetDiff(t *testing.T) {
	tests := []struct {
		name    string
		source  []string
		target  []string
		added   []string
		removed []string
		common  []string
	}{
		{
			name:   "both empty",
			source: nil,
			target: nil,
		},
		{
			name:    "source empty",
			source:  nil,
			target:  []string{"a", "b"},
			added:   []string{"a", "b"},
			removed: nil,
			common:  nil,
		},
		{
			name:    "target empty",
			source:  []string{"a", "b"},
			target:  nil,
			added:   nil,
			removed: []string{"a", "b"},
			common:  nil,
		},
		{
			name:    "identical",
			source:  []string{"a", "b"},
			target:  []string{"a", "b"},
			added:   nil,
			removed: nil,
			common:  []string{"a", "b"},
		},
		{
			name:    "disjoint",
			source:  []string{"a", "b"},
			target:  []string{"c", "d"},
			added:   []string{"c", "d"},
			removed: []string{"a", "b"},
			common:  nil,
		},
		{
			name:    "mixed overlap",
			source:  []string{"a", "b", "c"},
			target:  []string{"b", "c", "d"},
			added:   []string{"d"},
			removed: []string{"a"},
			common:  []string{"b", "c"},
		},
		{
			name:    "duplicates collapsed",
			source:  []string{"a", "a", "b"},
			target:  []string{"b", "b", "c"},
			added:   []string{"c"},
			removed: []string{"a"},
			common:  []string{"b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed, common := SetDiff(tt.source, tt.target)
			if !equalSlices(added, tt.added) {
				t.Errorf("added = %v, want %v", added, tt.added)
			}
			if !equalSlices(removed, tt.removed) {
				t.Errorf("removed = %v, want %v", removed, tt.removed)
			}
			if !equalSlices(common, tt.common) {
				t.Errorf("common = %v, want %v", common, tt.common)
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
