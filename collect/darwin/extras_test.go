//go:build darwin

package darwin

import (
	"testing"
	"time"
)

func TestParseNettopKeysByPID(t *testing.T) {
	got := parseNettop([]byte(",bytes_in,bytes_out,\nGoogle Chrome H.812,1200,300,\nbad line\nx.notapid,1,2,\nsyslogd.354,0,1339,\n"))
	if len(got) != 2 || got[812] != (netBytes{1200, 300}) || got[354] != (netBytes{0, 1339}) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseImageDisks(t *testing.T) {
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>images</key><array>
<dict><key>image-path</key><string>/Users/a/Pulse.dmg</string><key>system-entities</key><array>
<dict><key>dev-entry</key><string>/dev/disk8</string></dict>
<dict><key>dev-entry</key><string>/dev/disk8s1</string><key>mount-point</key><string>/Volumes/Pulse</string></dict>
</array></dict></array></dict></plist>`
	got, err := parseImageDisks([]byte(plist))
	if err != nil || len(got) != 1 || !got["disk8"] {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParseAPFSVolumeNames(t *testing.T) {
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>Containers</key><array><dict>
<key>ContainerReference</key><string>disk3</string><key>Volumes</key><array>
<dict><key>DeviceIdentifier</key><string>disk3s1</string><key>Name</key><string>Macintosh HD</string></dict>
<dict><key>DeviceIdentifier</key><string>disk3s5</string><key>Name</key><string>Data</string></dict>
</array></dict></array></dict></plist>`
	names, err := parseAPFSVolumeNames([]byte(plist))
	if err != nil || names["disk3s1"] != "Macintosh HD" || names["disk3s5"] != "Data" {
		t.Fatalf("got %v, %v", names, err)
	}
	c := &collector{apfsNames: names}
	// The root is the sealed snapshot disk3s1s1 of the volume disk3s1.
	if n := c.volumeName(t.Context(), "/dev/disk3s1s1", "/", "apfs"); n != "Macintosh HD" {
		t.Fatalf("root name %q", n)
	}
	if n := c.volumeName(t.Context(), "/dev/disk8s1", "/Volumes/Pulse", "hfs"); n != "Pulse" {
		t.Fatalf("image name %q", n)
	}
}

func TestInterfaceKind(t *testing.T) {
	kinds := map[string]netKind{"en0": {kind: kindOf("IEEE80211"), label: "Wi-Fi"}}
	if k := interfaceKind(kinds, "en0"); k.kind != "wifi" || k.label != "Wi-Fi" {
		t.Fatalf("en0 %+v", k)
	}
	if k := interfaceKind(kinds, "utun8"); k.kind != "vpn" {
		t.Fatalf("utun8 %+v", k)
	}
	if k := interfaceKind(kinds, "awdl0"); k.kind != "" {
		t.Fatalf("awdl0 %+v", k)
	}
}

func TestPageRate(t *testing.T) {
	if r := pageRate(100, 160, 2*time.Second); r == nil || *r != 30 {
		t.Fatalf("rate %v", r)
	}
	if pageRate(160, 100, time.Second) != nil || pageRate(1, 2, 0) != nil {
		t.Fatal("a reset or an empty interval must be unknown")
	}
}
