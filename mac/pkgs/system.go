package pkgs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"hotwatch/mac/exec"
)

// Streamer starts a long task and streams its output (exec.Streaming).
type Streamer func(ctx context.Context, name string, args ...string) (<-chan string, func() error, error)

// System is the Manager over the brew and mise found on this Mac. Targets
// are "kind:name" ("formula:go", "cask:iterm2", "mise:node"); the kind
// picks the tool and its flags, the name is checked before it reaches argv.
type System struct {
	run    exec.Runner
	stream Streamer

	once sync.Once
	have map[string]bool   // tool → found
	bin  map[string]string // tool → absolute path ("" in tests: the bare name)
}

var _ Manager = (*System)(nil)

// NewManager returns a System; nil run and stream use os/exec (30 s for a
// listing, streaming for tasks, whose deadline the action runner sets).
func NewManager(run exec.Runner, stream Streamer) *System {
	if run == nil {
		run = exec.System(30 * time.Second)
	}
	if stream == nil {
		stream = exec.Streaming
	}
	return &System{run: run, stream: stream}
}

// toolDirs are where Homebrew and mise put their binaries. An app launched
// from Finder has a minimal PATH, so they are looked up here, not on PATH.
func toolDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), filepath.Join(home, ".local", "share", "mise", "bin")}
}

func (s *System) find() {
	s.once.Do(func() {
		if s.have != nil {
			return // set by a test
		}
		s.have, s.bin = map[string]bool{}, map[string]string{}
		for _, tool := range []string{"brew", "mise"} {
			for _, dir := range toolDirs() {
				p := filepath.Join(dir, tool)
				if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
					s.have[tool], s.bin[tool] = true, p
					break
				}
			}
		}
	})
}

func (s *System) path(tool string) string {
	if p := s.bin[tool]; p != "" {
		return p
	}
	return tool
}

// Available reports whether brew and mise are installed.
func (s *System) Available() (brew, mise bool) {
	s.find()
	return s.have["brew"], s.have["mise"]
}

// List reads Homebrew's and mise's installed packages. A tool that fails is
// reported in the error; what the other one listed is still returned.
func (s *System) List(ctx context.Context) ([]Package, error) {
	s.find()
	var out []Package
	var errs []error
	if s.have["brew"] {
		src, err := s.run(ctx, s.path("brew"), "info", "--json=v2", "--installed")
		if err == nil {
			var ps []Package
			if ps, err = parseBrewInfo(src); err == nil {
				out = append(out, ps...)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("brew: %w", err))
		}
	}
	if s.have["mise"] {
		ls, err := s.run(ctx, s.path("mise"), "ls", "--json")
		var outdated []byte
		if err == nil {
			outdated, err = s.run(ctx, s.path("mise"), "outdated", "--json")
		}
		if err == nil {
			var ps []Package
			if ps, err = parseMise(ls, outdated); err == nil {
				out = append(out, ps...)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("mise: %w", err))
		}
	}
	return out, errors.Join(errs...)
}

// tokenRE is what a formula, cask or tool name may look like: Homebrew
// tokens (with an optional tap "user/repo/" and "@version") and mise tool
// names. Anything else is refused before it reaches argv.
var tokenRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._+-]{0,127}(/[A-Za-z0-9][A-Za-z0-9@._+-]{0,127}){0,2}$`)

// Run starts verb on target: upgrade and remove take "kind:name";
// upgrade-all and prune take "" (Homebrew) or "mise:".
func (s *System) Run(ctx context.Context, verb, target string) (<-chan string, func() error, error) {
	s.find()
	kind, name, _ := strings.Cut(target, ":")
	if name != "" && !tokenRE.MatchString(name) {
		return nil, nil, fmt.Errorf("pkgs: %q is not a package name", name)
	}
	tool, args := "brew", []string(nil)
	if kind == "mise" {
		tool = "mise"
	}
	cask := func(a ...string) []string {
		if kind == "cask" {
			return append(append(a[:1:1], "--cask"), a[1:]...)
		}
		return a
	}
	switch {
	case verb == "upgrade" && name != "" && tool == "brew":
		args = cask("upgrade", name)
	case verb == "remove" && name != "" && tool == "brew":
		args = cask("uninstall", name)
	case verb == "upgrade-all" && tool == "brew" && kind == "cask":
		args = []string{"upgrade", "--cask"} // the apps Homebrew installed
	case verb == "upgrade-all" && tool == "brew":
		args = []string{"upgrade"}
	case verb == "prune" && tool == "brew":
		args = []string{"cleanup", "--prune=all"}
	case verb == "upgrade" && name != "" && tool == "mise":
		args = []string{"upgrade", name}
	case verb == "remove" && name != "" && tool == "mise":
		args = []string{"uninstall", "--all", name}
	case verb == "upgrade-all" && tool == "mise":
		args = []string{"upgrade"}
	case verb == "prune" && tool == "mise":
		args = []string{"prune", "--yes"}
	default:
		return nil, nil, fmt.Errorf("pkgs: cannot %s %q", verb, target)
	}
	if !s.have[tool] {
		return nil, nil, fmt.Errorf("pkgs: %s is not installed", tool)
	}
	return s.stream(ctx, s.path(tool), args...)
}

// parseBrewInfo reads `brew info --json=v2 --installed`: installed formulae
// (their newest installed version) and casks.
func parseBrewInfo(src []byte) ([]Package, error) {
	var doc struct {
		Formulae []struct {
			Name      string `json:"name"`
			Desc      string `json:"desc"`
			Homepage  string `json:"homepage"`
			Tap       string `json:"tap"`
			Outdated  bool   `json:"outdated"`
			Installed []struct {
				Version string `json:"version"`
				Time    int64  `json:"time"`
			} `json:"installed"`
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
		} `json:"formulae"`
		Casks []struct {
			Token         string            `json:"token"`
			Desc          string            `json:"desc"`
			Homepage      string            `json:"homepage"`
			Tap           string            `json:"tap"`
			Version       string            `json:"version"`
			Installed     string            `json:"installed"`
			InstalledTime int64             `json:"installed_time"`
			Outdated      bool              `json:"outdated"`
			AutoUpdates   bool              `json:"auto_updates"`
			Artifacts     []json.RawMessage `json:"artifacts"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("brew info: %w", err)
	}
	unix := func(s int64) time.Time {
		if s <= 0 {
			return time.Time{}
		}
		return time.Unix(s, 0)
	}
	var out []Package
	for _, f := range doc.Formulae {
		if len(f.Installed) == 0 {
			continue
		}
		last := f.Installed[len(f.Installed)-1]
		out = append(out, Package{Name: f.Name, Kind: "formula", Version: last.Version,
			Newest: f.Versions.Stable, Outdated: f.Outdated, Desc: f.Desc, Homepage: f.Homepage, Tap: f.Tap,
			Installed: unix(last.Time)})
	}
	for _, c := range doc.Casks {
		if c.Installed == "" {
			continue
		}
		out = append(out, Package{Name: c.Token, Kind: "cask", Version: c.Installed, Newest: c.Version, Outdated: c.Outdated,
			Desc: c.Desc, Homepage: c.Homepage, Tap: c.Tap, Installed: unix(c.InstalledTime), AutoUpdates: c.AutoUpdates,
			Apps: caskApps(c.Artifacts)})
	}
	return out, nil
}

// caskApps reads the app bundles among a cask's artifacts: entries shaped
// {"app": ["Name.app", …]}; anything else is skipped.
func caskApps(arts []json.RawMessage) []string {
	var out []string
	for _, a := range arts {
		var art struct {
			App []json.RawMessage `json:"app"`
		}
		if json.Unmarshal(a, &art) != nil {
			continue
		}
		for _, x := range art.App {
			var name string
			if json.Unmarshal(x, &name) == nil && strings.HasSuffix(name, ".app") {
				out = append(out, filepath.Base(name))
			}
		}
	}
	return out
}

// parseMise reads `mise ls --json` (tool → installed versions; the active
// one, else the first) and `mise outdated --json` (tool → latest).
func parseMise(ls, outdated []byte) ([]Package, error) {
	var tools map[string][]struct {
		Version string `json:"version"`
		Active  bool   `json:"active"`
	}
	if err := json.Unmarshal(ls, &tools); err != nil {
		return nil, fmt.Errorf("mise ls: %w", err)
	}
	var later map[string]struct {
		Latest string `json:"latest"`
	}
	if len(strings.TrimSpace(string(outdated))) > 0 {
		if err := json.Unmarshal(outdated, &later); err != nil {
			return nil, fmt.Errorf("mise outdated: %w", err)
		}
	}
	names := make([]string, 0, len(tools))
	for n := range tools {
		names = append(names, n)
	}
	slices.Sort(names)
	var out []Package
	for _, n := range names {
		vs := tools[n]
		if len(vs) == 0 {
			continue
		}
		v := vs[0].Version
		for _, x := range vs {
			if x.Active {
				v = x.Version
				break
			}
		}
		p := Package{Name: n, Kind: "mise", Version: v, Newest: v}
		if o, ok := later[n]; ok && o.Latest != "" && o.Latest != v {
			p.Newest, p.Outdated = o.Latest, true
		}
		out = append(out, p)
	}
	return out, nil
}
