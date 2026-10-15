// Package pkgs lists Homebrew formulae and casks and mise tools and runs
// their upgrade, remove and prune tasks. The brew and mise readers land with
// the Packages page; the contract and the Fake live here.
package pkgs

import "context"

// Package is one installed formula, cask or mise tool.
type Package struct {
	Name, Kind, Version, Newest string // Kind "formula" | "cask" | "mise"
	Outdated                    bool
}

// Manager lists packages and runs tasks on them. Run starts the task and
// streams its combined output line by line; the returned wait reports the
// exit error once the lines are drained. Verbs: upgrade <name>,
// upgrade-all, remove <name>, prune.
type Manager interface {
	List(ctx context.Context) ([]Package, error)
	Available() (brew, mise bool)
	Run(ctx context.Context, verb, name string) (<-chan string, func() error, error)
}
