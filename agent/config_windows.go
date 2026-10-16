package agent

import (
	"errors"
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
// and nothing to anyone else; P stops entries inherited from the folder.
// privateDirDACL is the same for the folder, inherited by what it holds.
const (
	privateDACL    = "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;OW)"
	privateDirDACL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;OW)"
)

// restrictFile replaces f's access list with privateDACL, the Windows
// equivalent of mode 0600.
func restrictFile(f *os.File) error { return setPrivate(f.Name(), privateDACL) }

func setPrivate(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return err
	}
	giveToAdmins(path)
	return nil
}

// giveToAdmins hands ownership to Administrators when allowed (an elevated
// admin), so the service account and every admin pass the owner check.
func giveToAdmins(path string) {
	if admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid); err == nil {
		_ = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, admins, nil, nil, nil)
	}
}

// PrepareConfigDir creates the config folder with privateDirDACL in one
// call, so nothing can be dropped into it before it is locked. Any user may
// create folders under %ProgramData%, so an existing folder must pass the
// same check as the config itself, and a junction or symlink is refused.
func PrepareConfigDir(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(privateDirDACL)
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	switch err := windows.CreateDirectory(name, sa); {
	case err == nil:
		giveToAdmins(dir)
		return nil
	case !errors.Is(err, windows.ERROR_ALREADY_EXISTS):
		return err
	}
	return checkDir(dir)
}

// checkDir refuses a folder that is a reparse point (junction, symlink),
// whose target could change after the check, or that is not private.
func checkDir(dir string) error {
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("%s is a junction or symbolic link; the config folder must be a plain folder", dir)
	}
	if attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return fmt.Errorf("%s is not a folder", dir)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%s: reading access list: %w", dir, err)
	}
	return checkSD(dir, sd, dirRights)
}

// keepOwner is a no-op: the access list written by restrictFile already
// lets the service account (SYSTEM) read the file.
func keepOwner(*os.File, string) error { return nil }

// Access bits that matter. A file: reading or changing its data, its
// access list or its owner, or deleting it. A folder: adding, renaming or
// deleting what it holds, or changing its access list or owner.
const (
	fileReadData    = 0x0001 // FILE_READ_DATA, FILE_LIST_DIRECTORY
	fileWriteData   = 0x0002 // FILE_WRITE_DATA, FILE_ADD_FILE
	fileAppendData  = 0x0004 // FILE_APPEND_DATA, FILE_ADD_SUBDIRECTORY
	fileDeleteChild = 0x0040
	accessDelete    = 0x00010000
	writeDAC        = 0x00040000
	writeOwner      = 0x00080000
	maximumAllowed  = 0x02000000
	genericAll      = 0x10000000
	genericWrite    = 0x40000000
	genericRead     = 0x80000000

	changeRights = fileWriteData | fileAppendData | accessDelete | writeDAC | writeOwner |
		maximumAllowed | genericAll | genericWrite
	fileRights = changeRights | fileReadData | genericRead
	dirRights  = changeRights | fileDeleteChild

	inheritOnlyAce = 0x08
)

// checkPrivate refuses a config file, or a folder holding it, that anyone
// but SYSTEM, Administrators and the agent's own account may read, change
// or own. The file is checked through the open handle.
func checkPrivate(f *os.File, path string, _ fs.FileInfo) error {
	const info = windows.DACL_SECURITY_INFORMATION | windows.OWNER_SECURITY_INFORMATION
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, info)
	if err != nil {
		return fmt.Errorf("%s: reading access list: %w", path, err)
	}
	if err := checkSD(path, sd, fileRights); err != nil {
		return err
	}
	return checkDir(filepath.Dir(path))
}

// trusted reports SYSTEM, Administrators, OWNER RIGHTS and the account the
// agent runs as.
func trusted(sid, self *windows.SID) bool {
	if self != nil && sid.Equals(self) {
		return true
	}
	for _, w := range []windows.WELL_KNOWN_SID_TYPE{windows.WinLocalSystemSid,
		windows.WinBuiltinAdministratorsSid, windows.WinCreatorOwnerRightsSid} {
		if sid.IsWellKnown(w) {
			return true
		}
	}
	return false
}

// checkSD fails when the owner is untrusted (an owner may always rewrite
// the access list) or an entry grants any of rights to an untrusted
// account. Entries of a kind other than a plain allow or deny are refused
// rather than interpreted.
func checkSD(path string, sd *windows.SECURITY_DESCRIPTOR, rights uint32) error {
	var self *windows.SID
	if u, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		self = u.User.Sid
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return fmt.Errorf("%s has no owner", path)
	}
	if !trusted(owner, self) {
		return fmt.Errorf("%s is owned by %s; only SYSTEM, Administrators or the agent's account may own it", path, owner.String())
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("%s has no access list (anyone may use it); rewrite it with hotwatch-agent init --rotate", path)
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Errorf("%s: reading access list: %w", path, err)
		}
		if ace.Header.AceFlags&inheritOnlyAce != 0 {
			continue // applies only to what a folder will hold
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue // only takes rights away
		case windows.ACCESS_ALLOWED_ACE_TYPE:
		default:
			return fmt.Errorf("%s has an access entry of kind %d that hotwatch does not accept", path, ace.Header.AceType)
		}
		if uint32(ace.Mask)&rights == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !trusted(sid, self) {
			return fmt.Errorf("%s grants access to %s; only SYSTEM, Administrators and the agent's account may have it", path, sid.String())
		}
	}
	return nil
}
