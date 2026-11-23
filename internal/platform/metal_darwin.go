//go:build !js && darwin

package platform

import (
	"sync"
	"unsafe"

	sdl "github.com/Zyko0/go-sdl3/sdl"
	"github.com/ebitengine/purego"
)

var (
	metalOnce sync.Once
	metalView unsafe.Pointer
	metalNew  func(*sdl.Window) unsafe.Pointer
	metalGet  func(unsafe.Pointer) unsafe.Pointer
	metalFree func(unsafe.Pointer)
)

func loadMetal() {
	lib, err := purego.Dlopen(sdlLibraryPath(), purego.RTLD_NOW)
	if err != nil {
		return
	}
	purego.RegisterLibFunc(&metalNew, lib, "SDL_Metal_CreateView")
	purego.RegisterLibFunc(&metalGet, lib, "SDL_Metal_GetLayer")
	purego.RegisterLibFunc(&metalFree, lib, "SDL_Metal_DestroyView")
}

func sdlMetalLayer(p unsafe.Pointer) unsafe.Pointer {
	metalOnce.Do(loadMetal)
	if metalNew == nil {
		return nil
	}
	if metalView == nil {
		metalView = metalNew((*sdl.Window)(p))
	}
	if metalView == nil || metalGet == nil {
		return nil
	}
	return metalGet(metalView)
}

func sdlFreeMetalView() {
	if metalView != nil && metalFree != nil {
		metalFree(metalView)
		metalView = nil
	}
}

func sdlExtraWindowFlags() uint64 { return uint64(sdl.WINDOW_METAL) }
