//go:build darwin

package apps

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParsePlistXML(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>CFBundleName</key><string>Sample</string>
	<key>CFBundleIdentifier</key><string>com.example.sample</string>
	<key>CFBundleShortVersionString</key><string>1.2.3</string>
	<key>CFBundleExecutable</key><string>Sample</string>
	<key>LSMinimumSystemVersion</key><string>13.0</string>
	<key>CFBundleIcons</key><dict><key>CFBundlePrimaryIcon</key><dict><key>CFBundleIconName</key><string>AppIcon</string></dict></dict>
</dict></plist>`
	got, err := parsePlist([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"CFBundleName": "Sample", "CFBundleIdentifier": "com.example.sample",
		"CFBundleShortVersionString": "1.2.3", "CFBundleExecutable": "Sample"} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	if _, ok := got["CFBundleIconName"]; ok {
		t.Error("nested keys leaked into the top level")
	}
}

func TestMachOArch(t *testing.T) {
	hdr := func(magic, cpu uint32) []byte {
		b := make([]byte, 32)
		binary.LittleEndian.PutUint32(b, magic)
		binary.LittleEndian.PutUint32(b[4:], cpu)
		return b
	}
	fat := make([]byte, 32)
	binary.BigEndian.PutUint32(fat, 0xcafebabe)
	for name, tc := range map[string]struct {
		b    []byte
		want string
	}{
		"arm64":     {hdr(0xfeedfacf, 0x0100000c), "Apple silicon"},
		"x86_64":    {hdr(0xfeedfacf, 0x01000007), "Intel"},
		"universal": {fat, "Universal"},
		"garbage":   {[]byte("not a binary"), ""},
	} {
		if got := machOArch(tc.b); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestScanFindsSafariAndSizesAndTrashesACopy(t *testing.T) {
	s := New(nil)
	ctx := context.Background()
	list, err := s.Apps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var safari *App
	for i := range list {
		if list[i].Name == "Safari" {
			safari = &list[i]
		}
	}
	if safari == nil {
		t.Fatalf("Safari missing among %d apps", len(list))
	}
	if safari.Arch != "Universal" && safari.Arch != "Apple silicon" {
		t.Fatalf("Safari arch %q", safari.Arch)
	}
	if safari.Version == "" || safari.BundleID != "com.apple.Safari" {
		t.Fatalf("Safari %+v", safari)
	}
	n, err := s.Size(ctx, safari.Bundle)
	if err != nil || n < 1<<20 {
		t.Fatalf("Size = %d %v", n, err)
	}
	// A throwaway bundle under a temp dir: scanned when the dir is listed,
	// and sized by walking it.
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Throwaway.app")
	os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(`<plist version="1.0"><dict><key>CFBundleName</key><string>Throwaway</string><key>CFBundleExecutable</key><string>t</string></dict></plist>`), 0o644)
	os.WriteFile(filepath.Join(bundle, "Contents", "MacOS", "t"), make([]byte, 4096), 0o755)
	s2 := New(nil)
	s2.Dirs = []string{dir}
	list2, err := s2.Apps(ctx)
	if err != nil || len(list2) != 1 || list2[0].Name != "Throwaway" || list2[0].Arch != "" {
		t.Fatalf("Apps = %+v %v", list2, err)
	}
	if n, err := s2.Size(ctx, bundle); err != nil || n < 4096 {
		t.Fatalf("Size = %d %v", n, err)
	}
	if _, err := s2.Size(ctx, "/etc"); err == nil {
		t.Fatal("Size walked a path outside the scanned dirs")
	}
	if err := s2.Uninstall(ctx, "/Applications/Safari.app"); err == nil {
		t.Fatal("Uninstall accepted a bundle outside the scanned dirs")
	}
	if testing.Short() {
		return
	}
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s2.Uninstall(ctx2, bundle); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatalf("bundle still present after Uninstall: %v", err)
	}
}
