package agent

import (
	"testing"

	"golang.org/x/sys/windows"
)

func assertPrivate(t *testing.T, p string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if got := sd.String(); got != privateDACL {
		t.Fatalf("access list %s, want %s", got, privateDACL)
	}
	if err := checkPrivate(p, nil); err != nil {
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
