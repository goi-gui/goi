//go:build !js && !cgo && !darwin

package platform

import "unsafe"

func sdlMetalLayer(unsafe.Pointer) unsafe.Pointer { return nil }
func sdlFreeMetalView()                           {}
func sdlExtraWindowFlags() uint64                 { return 0 }
