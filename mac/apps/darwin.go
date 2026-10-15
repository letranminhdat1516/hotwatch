package apps

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hotwatch/mac/exec"
)

// Darwin scans the application folders of this Mac. It is the Scanner the
// app uses; tests point Dirs at a temporary folder.
type Darwin struct {
	// Dirs are the folders scanned one level deep for *.app bundles.
	Dirs []string
	// BrewPrefixes are where Homebrew keeps its Caskroom; an app found there
	// is reported as installed by Homebrew.
	BrewPrefixes []string
	run          exec.Runner

	mu    sync.Mutex
	sizes map[string]uint64
}

// New returns a scanner over the standard folders; run executes plutil,
// mdls and osascript (nil: os/exec with a 3 s timeout).
func New(run exec.Runner) *Darwin {
	if run == nil {
		run = exec.System(3 * time.Second)
	}
	home, _ := os.UserHomeDir()
	return &Darwin{
		Dirs:         []string{"/Applications", "/Applications/Utilities", filepath.Join(home, "Applications"), "/System/Applications"},
		BrewPrefixes: []string{"/opt/homebrew", "/usr/local"},
		run:          run,
		sizes:        map[string]uint64{},
	}
}

var _ Scanner = (*Darwin)(nil)

// Apps lists the bundles in Dirs with their plist facts, architecture,
// source and last-opened time; sizes are left for Size.
func (d *Darwin) Apps(ctx context.Context) ([]App, error) {
	var out []App
	casks := d.caskApps()
	for _, dir := range d.Dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a folder that is not there (no ~/Applications) is not an error
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".app") {
				continue
			}
			bundle := filepath.Join(dir, e.Name())
			a, ok := d.read(ctx, bundle)
			if !ok {
				continue
			}
			switch {
			case fileExists(filepath.Join(bundle, "Contents", "_MASReceipt", "receipt")):
				a.Source = "App Store"
			case casks[e.Name()]:
				a.Source = "Homebrew"
			}
			out = append(out, a)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	d.lastOpened(ctx, out)
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// read is one bundle's facts from its Info.plist and executable.
func (d *Darwin) read(ctx context.Context, bundle string) (App, bool) {
	src, err := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist"))
	if err != nil {
		return App{}, false
	}
	info, err := readPlist(ctx, d.run, filepath.Join(bundle, "Contents", "Info.plist"), src)
	if err != nil {
		info = map[string]string{}
	}
	name := info["CFBundleDisplayName"]
	if name == "" {
		name = info["CFBundleName"]
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(bundle), ".app")
	}
	a := App{Name: name, Bundle: bundle, BundleID: info["CFBundleIdentifier"], Version: info["CFBundleShortVersionString"]}
	if a.Version == "" {
		a.Version = info["CFBundleVersion"]
	}
	if exe := info["CFBundleExecutable"]; exe != "" {
		if hdr, err := readHead(filepath.Join(bundle, "Contents", "MacOS", exe), 8); err == nil {
			a.Arch = machOArch(hdr)
		}
	}
	return a, true
}

// machOArch names the architecture of a Mach-O header: a fat binary is
// "Universal", a thin arm64 one "Apple silicon", x86_64 "Intel"; anything
// else is "".
func machOArch(hdr []byte) string {
	if len(hdr) < 8 {
		return ""
	}
	switch binary.BigEndian.Uint32(hdr) {
	case 0xcafebabe, 0xcafebabf: // FAT_MAGIC, FAT_MAGIC_64
		return "Universal"
	}
	if binary.LittleEndian.Uint32(hdr) != 0xfeedfacf { // MH_MAGIC_64
		return ""
	}
	switch binary.LittleEndian.Uint32(hdr[4:]) {
	case 0x0100000c: // CPU_TYPE_ARM64
		return "Apple silicon"
	case 0x01000007: // CPU_TYPE_X86_64
		return "Intel"
	}
	return ""
}

func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	m, err := f.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:m], nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// caskApps is the set of app bundle names Homebrew's Caskroom holds, read
// from the file system (no brew call): Caskroom/<token>/<version>/<App>.app.
func (d *Darwin) caskApps() map[string]bool {
	out := map[string]bool{}
	for _, prefix := range d.BrewPrefixes {
		tokens, err := os.ReadDir(filepath.Join(prefix, "Caskroom"))
		if err != nil {
			continue
		}
		for _, tok := range tokens {
			versions, err := os.ReadDir(filepath.Join(prefix, "Caskroom", tok.Name()))
			if err != nil {
				continue
			}
			for _, ver := range versions {
				if strings.HasPrefix(ver.Name(), ".") {
					continue
				}
				items, _ := os.ReadDir(filepath.Join(prefix, "Caskroom", tok.Name(), ver.Name()))
				for _, it := range items {
					if strings.HasSuffix(it.Name(), ".app") {
						out[it.Name()] = true
					}
				}
			}
		}
	}
	return out
}

// lastOpened fills LastOpened from Spotlight's kMDItemLastUsedDate in one
// mdls call; a failure leaves the times zero.
func (d *Darwin) lastOpened(ctx context.Context, list []App) {
	if len(list) == 0 || d.run == nil {
		return
	}
	args := []string{"-name", "kMDItemLastUsedDate"}
	for _, a := range list {
		args = append(args, a.Bundle)
	}
	out, err := d.run(ctx, "mdls", args...)
	if err != nil {
		return
	}
	// One "kMDItemLastUsedDate = 2026-10-15 01:55:00 +0000" line per bundle,
	// in order; "(null)" when Spotlight has none.
	i := 0
	for _, line := range strings.Split(string(out), "\n") {
		_, val, ok := strings.Cut(line, "= ")
		if !ok || i >= len(list) {
			continue
		}
		if t, err := time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(val)); err == nil {
			list[i].LastOpened = t
		}
		i++
	}
}

// inDirs reports whether bundle is a *.app directly under one of Dirs: the
// only paths Size walks and Uninstall trashes.
func (d *Darwin) inDirs(bundle string) bool {
	if !strings.HasSuffix(bundle, ".app") || !filepath.IsAbs(bundle) {
		return false
	}
	clean := filepath.Clean(bundle)
	for _, dir := range d.Dirs {
		if filepath.Dir(clean) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// Size walks the bundle once and caches the total.
func (d *Darwin) Size(ctx context.Context, bundle string) (uint64, error) {
	if !d.inDirs(bundle) {
		return 0, fmt.Errorf("apps: %q is not an application bundle in a scanned folder", bundle)
	}
	d.mu.Lock()
	n, ok := d.sizes[bundle]
	d.mu.Unlock()
	if ok {
		return n, nil
	}
	// /Applications/Safari.app and friends are links into a cryptex: walk
	// what they point at.
	root, err := filepath.EvalSymlinks(bundle)
	if err != nil {
		root = bundle
	}
	var total uint64
	err = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable parts are skipped, not fatal
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e.Type().IsRegular() {
			if info, err := e.Info(); err == nil {
				total += uint64(info.Size())
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	d.mu.Lock()
	d.sizes[bundle] = total
	d.mu.Unlock()
	return total, nil
}

// Uninstall asks Finder to move the bundle to the Trash: Finder owns the
// administrator prompt for bundles the user cannot write. The path travels
// as an argument of the script, never inside it.
func (d *Darwin) Uninstall(ctx context.Context, bundle string) error {
	if !d.inDirs(bundle) {
		return fmt.Errorf("apps: %q is not an application bundle in a scanned folder", bundle)
	}
	if strings.HasPrefix(bundle, "/System/") {
		return errors.New("apps: system applications cannot be removed")
	}
	run := d.run
	if run == nil {
		run = exec.System(time.Minute)
	}
	_, err := run(ctx, "osascript",
		"-e", "on run argv",
		"-e", "tell application \"Finder\" to delete (POSIX file (item 1 of argv) as alias)",
		"-e", "end run",
		bundle)
	if err != nil {
		return err
	}
	d.mu.Lock()
	delete(d.sizes, bundle)
	d.mu.Unlock()
	return nil
}
