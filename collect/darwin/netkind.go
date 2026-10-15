//go:build darwin

package darwin

import (
	"strings"
	"sync"

	"github.com/ebitengine/purego"

	"hotwatch/internal/cf"
)

// netKind is what the system calls an interface: its kind and its name.
type netKind struct{ kind, label string }

var scFns = sync.OnceValues(func() (struct {
	copyAll func() cf.Ref
	bsdName func(cf.Ref) cf.Ref
	ifType  func(cf.Ref) cf.Ref
	display func(cf.Ref) cf.Ref
}, bool) {
	var f struct {
		copyAll func() cf.Ref
		bsdName func(cf.Ref) cf.Ref
		ifType  func(cf.Ref) cf.Ref
		display func(cf.Ref) cf.Ref
	}
	if cf.Load() != nil {
		return f, false
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/SystemConfiguration.framework/SystemConfiguration", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return f, false
	}
	purego.RegisterLibFunc(&f.copyAll, lib, "SCNetworkInterfaceCopyAll")
	purego.RegisterLibFunc(&f.bsdName, lib, "SCNetworkInterfaceGetBSDName")
	purego.RegisterLibFunc(&f.ifType, lib, "SCNetworkInterfaceGetInterfaceType")
	purego.RegisterLibFunc(&f.display, lib, "SCNetworkInterfaceGetLocalizedDisplayName")
	return f, true
})

// readNetKinds asks SystemConfiguration for the kind and name of every
// interface it manages (Wi-Fi, Ethernet, Thunderbolt Bridge), by BSD name.
func readNetKinds() map[string]netKind {
	f, ok := scFns()
	if !ok {
		return nil
	}
	all := f.copyAll()
	if all == 0 {
		return nil
	}
	defer cf.Release(all)
	out := map[string]netKind{}
	for _, it := range cf.ArrayValues(all) {
		name, _ := cf.ToGo(f.bsdName(it)).(string) // Get: not owned
		if name == "" {
			continue
		}
		typ, _ := cf.ToGo(f.ifType(it)).(string)
		label, _ := cf.ToGo(f.display(it)).(string)
		out[name] = netKind{kind: kindOf(typ), label: label}
	}
	return out
}

// kindOf maps an SCNetworkInterface type to the interface kinds the UI
// knows.
func kindOf(scType string) string {
	switch scType {
	case "IEEE80211":
		return "wifi"
	case "Ethernet":
		return "ethernet"
	case "Bridge":
		return "bridge"
	}
	return ""
}

// interfaceKind is the kind and label of name: SystemConfiguration's when
// it manages the interface, "vpn" for tunnels; "" otherwise.
func interfaceKind(kinds map[string]netKind, name string) netKind {
	if k, ok := kinds[name]; ok && k.kind != "" {
		return k
	}
	if strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "ipsec") || strings.HasPrefix(name, "ppp") {
		return netKind{kind: "vpn", label: "VPN"}
	}
	return netKind{}
}
