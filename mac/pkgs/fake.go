package pkgs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
)

// Fake serves Pkgs, reports Brew/Mise as available, and plays Log for every
// task, failing with "exit status 1" when Fail is set. Ran records the tasks.
type Fake struct {
	Mu         sync.Mutex
	Pkgs       []Package
	Brew, Mise bool
	Log        []string
	Fail       bool
	Err        error // returned by List and Run while non-nil
	Ran        []string
}

// List returns a copy of Pkgs.
func (f *Fake) List(context.Context) ([]Package, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return slices.Clone(f.Pkgs), nil
}

// Available reports Brew and Mise.
func (f *Fake) Available() (bool, bool) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return f.Brew, f.Mise
}

// Run records "verb name" and streams Log; on success it clears Outdated
// on the named package, as a real upgrade would.
func (f *Fake) Run(ctx context.Context, verb, name string) (<-chan string, func() error, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return nil, nil, f.Err
	}
	f.Ran = append(f.Ran, strings.TrimSpace(verb+" "+name))
	ch := make(chan string, len(f.Log))
	for _, l := range f.Log {
		ch <- l
	}
	close(ch)
	fail := f.Fail
	return ch, func() error {
		if fail {
			return errors.New("brew: exit status 1")
		}
		f.Mu.Lock()
		defer f.Mu.Unlock()
		for i := range f.Pkgs {
			if f.Pkgs[i].Name == name || verb == "upgrade-all" {
				f.Pkgs[i].Outdated = false
				f.Pkgs[i].Version = f.Pkgs[i].Newest
			}
		}
		return nil
	}, nil
}
