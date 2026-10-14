package pages

import (
	"slices"

	"hotwatch/model"
)

// procTree is the process list as parents and children, by index into the
// list.
type procTree struct {
	Roots    []int
	Children map[int][]int
}

// buildTree arranges the processes by PPID. keep, when not nil, limits the
// tree to the processes it marks and their ancestors; less orders siblings.
// A process whose parent is not in the list, is itself, is part of a cycle,
// or started before the process it names as parent (the parent's PID was
// reused) is a root.
func buildTree(ps []model.Process, keep []bool, less func(a, b int) bool) procTree {
	byPID := make(map[int32]int, len(ps))
	for i := len(ps) - 1; i >= 0; i-- {
		byPID[ps[i].PID] = i // the first of equal PIDs wins
	}
	parent := make([]int, len(ps))
	for i, p := range ps {
		parent[i] = -1
		j, ok := byPID[p.PPID]
		if !ok || j == i {
			continue
		}
		if q := ps[j]; !q.StartTime.IsZero() && !p.StartTime.IsZero() && q.StartTime.After(p.StartTime) {
			continue
		}
		parent[i] = j
	}
	for i := range parent { // break cycles at the first node that finds one
		steps := 0
		for j := parent[i]; j >= 0; j = parent[j] {
			if steps++; steps > len(ps) {
				parent[i] = -1
				break
			}
		}
	}
	in := make([]bool, len(ps))
	for i := range ps {
		if keep != nil && !keep[i] {
			continue
		}
		for j := i; j >= 0 && !in[j]; j = parent[j] {
			in[j] = true
		}
	}
	tree := procTree{Children: map[int][]int{}}
	for i := range ps {
		switch {
		case !in[i]:
		case parent[i] < 0:
			tree.Roots = append(tree.Roots, i)
		default:
			tree.Children[parent[i]] = append(tree.Children[parent[i]], i)
		}
	}
	cmp := func(a, b int) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		}
		return 0
	}
	slices.SortStableFunc(tree.Roots, cmp)
	for _, c := range tree.Children {
		slices.SortStableFunc(c, cmp)
	}
	return tree
}
