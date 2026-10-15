//go:build darwin && arm64

package symbols

import (
	"fmt"
	"image"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/ui"
)

// System renders symbols through AppKit and caches them by name, size and
// colour.
type System struct {
	cache map[string]*ui.Bitmap
}

// New returns an empty cache over AppKit.
func New() *System { return &System{cache: map[string]*ui.Bitmap{}} }

var _ Source = (*System)(nil)

// Symbol renders (once) and returns the tinted bitmap, nil when unknown.
func (s *System) Symbol(name string, px int, color ui.Color) *ui.Bitmap {
	key := fmt.Sprintf("%s|%d|%02x%02x%02x%02x", name, px, color.R, color.G, color.B, color.A)
	if b, ok := s.cache[key]; ok {
		return b
	}
	var b *ui.Bitmap
	if img := render(name, px, color); img != nil {
		b = ui.NewBitmap(img)
	}
	s.cache[key] = b // a miss is cached too: asking again would fail again
	return b
}

var (
	selAlloc      = objc.RegisterName("alloc")
	selInit       = objc.RegisterName("init")
	selDrain      = objc.RegisterName("drain")
	selSymbol     = objc.RegisterName("imageWithSystemSymbolName:accessibilityDescription:")
	selConfig     = objc.RegisterName("configurationWithPointSize:weight:")
	selWithConfig = objc.RegisterName("imageWithSymbolConfiguration:")
	selCGImage    = objc.RegisterName("CGImageForProposedRect:context:hints:")
	selInitBytes  = objc.RegisterName("initWithBytes:length:encoding:")
	selAutorel    = objc.RegisterName("autorelease")
)

// fontWeightSemibold is NSFontWeightSemibold.
const fontWeightSemibold = 0.3

type cg struct {
	space   func() uintptr
	release func(uintptr)
	ctx     func(data unsafe.Pointer, w, h, bpc, bpr uintptr, space uintptr, info uint32) uintptr
	draw    func(ctx uintptr, x, y, w, h float64, img uintptr)
	interp  func(ctx uintptr, q int32)
	ctxFree func(uintptr)
	imgW    func(uintptr) uintptr
	imgH    func(uintptr) uintptr
}

var load = sync.OnceValue(func() *cg {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return nil
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil
	}
	f := &cg{}
	purego.RegisterLibFunc(&f.space, lib, "CGColorSpaceCreateDeviceRGB")
	purego.RegisterLibFunc(&f.release, lib, "CGColorSpaceRelease")
	purego.RegisterLibFunc(&f.ctx, lib, "CGBitmapContextCreate")
	purego.RegisterLibFunc(&f.draw, lib, "CGContextDrawImage")
	purego.RegisterLibFunc(&f.interp, lib, "CGContextSetInterpolationQuality")
	purego.RegisterLibFunc(&f.ctxFree, lib, "CGContextRelease")
	purego.RegisterLibFunc(&f.imgW, lib, "CGImageGetWidth")
	purego.RegisterLibFunc(&f.imgH, lib, "CGImageGetHeight")
	return f
})

// render draws symbol name px square: semibold, scaled to fit with its
// aspect kept, every pixel in color at the symbol's coverage.
func render(name string, px int, color ui.Color) *image.RGBA {
	f := load()
	if f == nil || px <= 0 {
		return nil
	}
	// An autorelease pool must be drained on the thread that made it: keep
	// this goroutine on one OS thread until it is.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)
	img := objc.ID(objc.GetClass("NSImage")).Send(selSymbol, nsString(name), 0)
	if img == 0 {
		return nil
	}
	conf := objc.ID(objc.GetClass("NSImageSymbolConfiguration")).Send(selConfig, float64(px), float64(fontWeightSemibold))
	if conf != 0 {
		if c := img.Send(selWithConfig, conf); c != 0 {
			img = c
		}
	}
	cgImg := uintptr(img.Send(selCGImage, 0, 0, 0))
	if cgImg == 0 {
		return nil
	}
	iw, ih := float64(f.imgW(cgImg)), float64(f.imgH(cgImg))
	if iw == 0 || ih == 0 {
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, px, px))
	space := f.space()
	defer f.release(space)
	ctx := f.ctx(unsafe.Pointer(&out.Pix[0]), uintptr(px), uintptr(px), 8, uintptr(out.Stride), space, 1|4<<12)
	if ctx == 0 {
		return nil
	}
	scale := min(float64(px)/iw, float64(px)/ih)
	w, h := iw*scale, ih*scale
	f.interp(ctx, 3)
	f.draw(ctx, (float64(px)-w)/2, (float64(px)-h)/2, w, h, cgImg)
	f.ctxFree(ctx)
	runtime.KeepAlive(out)
	// Tint: the symbol's alpha is its coverage; every pixel becomes the
	// colour at that coverage (premultiplied, as image.RGBA is).
	for i := 0; i < len(out.Pix); i += 4 {
		a := uint32(out.Pix[i+3]) * uint32(color.A) / 255
		out.Pix[i+0] = uint8(uint32(color.R) * a / 255)
		out.Pix[i+1] = uint8(uint32(color.G) * a / 255)
		out.Pix[i+2] = uint8(uint32(color.B) * a / 255)
		out.Pix[i+3] = uint8(a)
	}
	return out
}

func nsString(s string) objc.ID {
	b := unsafe.StringData(s)
	str := objc.ID(objc.GetClass("NSString")).Send(selAlloc).Send(selInitBytes, uintptr(unsafe.Pointer(b)), uintptr(len(s)), 4)
	runtime.KeepAlive(s)
	return str.Send(selAutorel)
}
