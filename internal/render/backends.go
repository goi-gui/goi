//go:build !nogpu && !js

package render

// The HAL backends register themselves through init, which is what turns a
// wgpu instance into a Vulkan, Metal, DX12 or software one. A browser has a
// single WebGPU backend and no HAL to register, so this file is native only.
import _ "github.com/gogpu/wgpu/hal/allbackends"
