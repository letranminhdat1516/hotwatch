package appicon

import "github.com/egoist/mygo/ui"

// Fake answers from Icons. A path in Pending answers false until Deliver,
// which is how tests play the asynchronous load of the real source. Asked
// records every Icon call in order.
type Fake struct {
	Icons   map[string]*ui.Bitmap
	Pending map[string]bool
	Asked   []string
}

// Icon answers from Icons unless the path is still Pending.
func (f *Fake) Icon(p string) (*ui.Bitmap, bool) {
	f.Asked = append(f.Asked, p)
	if f.Pending[p] {
		return nil, false
	}
	b, ok := f.Icons[p]
	if !ok || b == nil {
		return nil, false
	}
	return b, true
}

// Deliver completes the pending load of p.
func (f *Fake) Deliver(p string) { delete(f.Pending, p) }
