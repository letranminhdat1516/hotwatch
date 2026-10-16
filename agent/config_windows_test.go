package agent

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func assertPrivate(t *testing.T, p string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if got := aces(sd.String()); got != aces(privateDACL) {
		t.Fatalf("access list %s, want %s", got, privateDACL)
	}
	if _, err := LoadConfig(p); err != nil {
		t.Fatal(err)
	}
}

// makePublic adds read access for Everyone (WD).
func makePublic(t *testing.T, p string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("D:(A;;FR;;;WD)(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func setDACL(t *testing.T, p, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func writeGood(t *testing.T) (dir, p string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "Hotwatch")
	if err := PrepareConfigDir(dir); err != nil {
		t.Fatal(err)
	}
	p = filepath.Join(dir, "agent.json")
	if err := WriteConfig(p, FileConfig{Listen: "127.0.0.1:7171", TokenSHA256: SHA256Hex("x")}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err != nil {
		t.Fatal(err)
	}
	return dir, p
}

func TestLoadConfigRejectsWritableByOthers(t *testing.T) {
	_, p := writeGood(t)
	setDACL(t, p, "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FW;;;BU)") // Users may write
	if _, err := LoadConfig(p); err == nil {
		t.Fatal("a config others may write must be rejected")
	}
}

func TestLoadConfigRejectsOpenDirectory(t *testing.T) {
	dir, p := writeGood(t)
	setDACL(t, dir, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;;0x6;;;BU)") // Users may add files
	if _, err := LoadConfig(p); err == nil {
		t.Fatal("a config in a folder others may write must be rejected")
	}
}

func TestPrepareConfigDirIsPrivate(t *testing.T) {
	dir, _ := writeGood(t)
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if got := aces(sd.String()); got != aces(privateDirDACL) {
		t.Fatalf("folder access list %s, want %s", got, privateDirDACL)
	}
}

func TestConfigRefusesJunctionFolder(t *testing.T) {
	_, p := writeGood(t)
	link := filepath.Join(t.TempDir(), "Hotwatch")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Dir(p)).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}
	if err := PrepareConfigDir(link); err == nil {
		t.Fatal("a junction must not be accepted as the config folder")
	}
	if _, err := LoadConfig(filepath.Join(link, "agent.json")); err == nil {
		t.Fatal("a config reached through a junction must be rejected")
	}
}

// aces is an SDDL access list without its flags ("D:PAI(...)" → "(...)"):
// Windows adds AI (auto-inherited) when it stores a protected list.
func aces(sddl string) string {
	if i := strings.IndexByte(sddl, '('); i >= 0 {
		return sddl[i:]
	}
	return sddl
}
