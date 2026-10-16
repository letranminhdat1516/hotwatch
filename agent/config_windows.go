package agent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func defaultConfigPath() string {
	dir := os.Getenv("ProgramData")
	if dir == "" {
		dir = `C:\ProgramData`
	}
	return filepath.Join(dir, "Hotwatch", "agent.json")
}

// privateDACL grants full control to SYSTEM, Administrators and the owner,
// and nothing to anyone else; P stops inherited entries from the folder.
const privateDACL = "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;OW)"

// restrictFile replaces f's access list with privateDACL, the Windows
// equivalent of mode 0600.
func restrictFile(f *os.File) error {
	sd, err := windows.SecurityDescriptorFromString(privateDACL)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(f.Name(), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// keepOwner is a no-op: the access list written by restrictFile already
// lets the service account (SYSTEM) read the file.
func keepOwner(*os.File, string) error { return nil }

// readRights are the access bits that let a holder read the file's data.
const readRights = windows.FILE_READ_DATA | windows.GENERIC_READ | windows.GENERIC_ALL | windows.MAXIMUM_ALLOWED

// checkPrivate refuses a file that anyone but SYSTEM, Administrators, the
// owner (and OWNER RIGHTS) or the user running the agent may read.
func checkPrivate(path string, _ fs.FileInfo) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%s: reading access list: %w", path, err)
	}
	owner, _, _ := sd.Owner()
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("%s has no access list (anyone may read it); rewrite it with hotwatch-agent init --rotate", path)
	}
	var self *windows.SID
	if u, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		self = u.User.Sid
	}
	allowed := []windows.WELL_KNOWN_SID_TYPE{windows.WinLocalSystemSid, windows.WinBuiltinAdministratorsSid, windows.WinCreatorOwnerRightsSid}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Errorf("%s: reading access list: %w", path, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask&readRights == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if (owner != nil && sid.Equals(owner)) || (self != nil && sid.Equals(self)) {
			continue
		}
		ok := false
		for _, w := range allowed {
			if sid.IsWellKnown(w) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%s is readable by %s; only SYSTEM, Administrators and its owner may read it", path, sid.String())
		}
	}
	return nil
}
