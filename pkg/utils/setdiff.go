// Package utils holds small generic helpers with no dependency on any
// domain package, shared across wireops packages.
package utils

// SetDiff compares source against target and reports: added (present in
// target, absent from source, in target's order), removed (present in
// source, absent from target, in source's order), and common (present in
// both, in target's order). Duplicate elements within either slice are
// collapsed.
func SetDiff[T comparable](source, target []T) (added, removed, common []T) {
	inSource := make(map[T]bool, len(source))
	for _, v := range source {
		inSource[v] = true
	}
	inTarget := make(map[T]bool, len(target))
	for _, v := range target {
		inTarget[v] = true
	}

	seen := make(map[T]bool, len(target))
	for _, v := range target {
		if seen[v] {
			continue
		}
		seen[v] = true
		if inSource[v] {
			common = append(common, v)
		} else {
			added = append(added, v)
		}
	}

	seen = make(map[T]bool, len(source))
	for _, v := range source {
		if seen[v] {
			continue
		}
		seen[v] = true
		if !inTarget[v] {
			removed = append(removed, v)
		}
	}

	return added, removed, common
}
