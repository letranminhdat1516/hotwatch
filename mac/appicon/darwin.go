//go:build darwin && arm64

package appicon

import (
	"image"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/ui"
)

// cacheSize is how many icons the darwin source keeps; beyond it the least
// recently asked for is dropped.
const cacheSize = 512

// workers is how many icons load at once: AppKit reads the bundle's .icns
// from disk, so two keep a 3,000-row list from starving the frame.
const workers = 2

// Darwin asks AppKit (NSWorkspace iconForFile:) for icons on worker
// goroutines and delivers each result to the main thread through post.
// Icon never blocks: a miss is queued and answers false until the delivery
// lands, after which the next frame gets the bitmap.
type Darwin struct {
	post func(func())
	px   int

	mu      sync.Mutex
	icons   map[string]*entry
	order   []string // least recently used first
	pending map[string]bool
	queue   chan string
	once    sync.Once
}

type entry struct {
	bm *ui.Bitmap // nil: the bundle has no usable icon
}

// NewDarwin returns a Source rendering icons at px pixels per side (the
// list shows them at 20 DIP; 40 covers Retina). post runs a function on the
// main thread, as mygo's win.Update does.
func NewDarwin(post func(func()), px int) *Darwin {
	if px <= 0 {
		px = 40
	}
	return &Darwin{post: post, px: px, icons: map[string]*entry{}, pending: map[string]bool{}, queue: make(chan string, 1024)}
}

// Icon answers from the cache; a miss schedules a load.
func (d *Darwin) Icon(bundle string) (*ui.Bitmap, bool) {
	if !Key(bundle) {
		return nil, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.icons[bundle]; ok {
		d.touch(bundle)
		return e.bm, e.bm != nil
	}
	if !d.pending[bundle] {
		d.pending[bundle] = true
		d.once.Do(d.start)
		select {
		case d.queue <- bundle:
		default:
			delete(d.pending, bundle) // queue full: asked again next frame
		}
	}
	return nil, false
}

// appKit loads AppKit once: the app has it through mygo, a test binary
// does not, and a class of an unloaded framework resolves to nil.
var appKit = sync.OnceValue(func() error {
	_, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return err
	}
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return err
	}
	purego.RegisterLibFunc(&cgColorSpaceCreateDeviceRGB, cg, "CGColorSpaceCreateDeviceRGB")
	purego.RegisterLibFunc(&cgColorSpaceRelease, cg, "CGColorSpaceRelease")
	purego.RegisterLibFunc(&cgBitmapContextCreate, cg, "CGBitmapContextCreate")
	purego.RegisterLibFunc(&cgContextSetInterpolation, cg, "CGContextSetInterpolationQuality")
	purego.RegisterLibFunc(&cgContextDrawImage, cg, "CGContextDrawImage")
	purego.RegisterLibFunc(&cgContextRelease, cg, "CGContextRelease")
	return nil
})

func (d *Darwin) start() {
	for i := 0; i < workers; i++ {
		go d.worker()
	}
}

func (d *Darwin) worker() {
	runtime.LockOSThread()
	for bundle := range d.queue {
		bm := load(bundle, d.px)
		d.post(func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			delete(d.pending, bundle)
			d.icons[bundle] = &entry{bm: bm}
			d.touch(bundle)
			for len(d.order) > cacheSize {
				delete(d.icons, d.order[0])
				d.order = d.order[1:]
			}
		})
	}
}

// touch moves bundle to the most recently used end. d.mu held.
func (d *Darwin) touch(bundle string) {
	for i, b := range d.order {
		if b == bundle {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
	d.order = append(d.order, bundle)
}

var (
	selSharedWorkspace = objc.RegisterName("sharedWorkspace")
	selIconForFile     = objc.RegisterName("iconForFile:")
	selIconForFileType = objc.RegisterName("iconForFileType:")
	selAlloc           = objc.RegisterName("alloc")
	selInit            = objc.RegisterName("init")
	selDrain           = objc.RegisterName("drain")
	selInitWithBytes   = objc.RegisterName("initWithBytes:length:encoding:")
	selAutorelease     = objc.RegisterName("autorelease")
	selCGImage         = objc.RegisterName("CGImageForProposedRect:context:hints:")
)

// CoreGraphics, bound once. On arm64 a CGRect (four doubles) travels in
// four floating-point registers, so CGContextDrawImage takes it as four
// float64 arguments; this file is arm64-only for that reason.
var (
	cgColorSpaceCreateDeviceRGB func() uintptr
	cgColorSpaceRelease         func(uintptr)
	cgBitmapContextCreate       func(data unsafe.Pointer, w, h, bitsPerComponent, bytesPerRow uintptr, space uintptr, info uint32) uintptr
	cgContextSetInterpolation   func(ctx uintptr, quality int32)
	cgContextDrawImage          func(ctx uintptr, x, y, w, h float64, image uintptr)
	cgContextRelease            func(uintptr)
)

const (
	nsUTF8StringEncoding = 4
	// kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big: RGBA bytes,
	// premultiplied, the layout of image.RGBA.
	cgRGBAPremultiplied = 1 | 4<<12
	cgInterpolationHigh = 3
)

// load renders the icon of bundle to a px×px bitmap: AppKit's NSImage for
// the bundle, its CGImage drawn scaled into a premultiplied RGBA context.
// nil when AppKit has nothing for it.
func load(bundle string, px int) *ui.Bitmap {
	if appKit() != nil {
		return nil
	}
	// An autorelease pool must be drained on the thread that made it: keep
	// this goroutine on one OS thread until it is.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	ws := objc.ID(objc.GetClass("NSWorkspace")).Send(selSharedWorkspace)
	var img objc.ID
	if bundle == Executable {
		img = ws.Send(selIconForFileType, nsString(Executable))
	} else {
		img = ws.Send(selIconForFile, nsString(bundle))
	}
	if img == 0 {
		return nil
	}
	cgImg := img.Send(selCGImage, 0, 0, 0) // the whole image, its best representation
	if cgImg == 0 {
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, px, px))
	space := cgColorSpaceCreateDeviceRGB()
	defer cgColorSpaceRelease(space)
	ctx := cgBitmapContextCreate(unsafe.Pointer(&out.Pix[0]), uintptr(px), uintptr(px), 8, uintptr(out.Stride), space, cgRGBAPremultiplied)
	if ctx == 0 {
		return nil
	}
	cgContextSetInterpolation(ctx, cgInterpolationHigh)
	cgContextDrawImage(ctx, 0, 0, float64(px), float64(px), uintptr(cgImg))
	cgContextRelease(ctx)
	runtime.KeepAlive(out)
	// CoreGraphics' origin is bottom-left: flip the rows.
	for y := 0; y < px/2; y++ {
		a, b := out.Pix[y*out.Stride:(y+1)*out.Stride], out.Pix[(px-1-y)*out.Stride:(px-y)*out.Stride]
		for i := range a {
			a[i], b[i] = b[i], a[i]
		}
	}
	return ui.NewBitmap(out)
}

// nsString is an autoreleased NSString of s.
func nsString(s string) objc.ID {
	b := unsafe.StringData(s)
	str := objc.ID(objc.GetClass("NSString")).Send(selAlloc).Send(selInitWithBytes,
		uintptr(unsafe.Pointer(b)), uintptr(len(s)), nsUTF8StringEncoding)
	runtime.KeepAlive(s)
	return str.Send(selAutorelease)
}
