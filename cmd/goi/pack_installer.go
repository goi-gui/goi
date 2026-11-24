//go:build !js && !wasip1

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goi-gui/goi/internal/img"
)

// packWindows builds the binary and wraps it in an NSIS installer. makensis
// itself runs on Linux, macOS and Windows, so the installer can be built
// anywhere the tool is installed.
func packWindows(dir string, opts packOptions, cfg config, icon, sdl string, goRun runFunc, runTool packExecFunc, stdout, stderr io.Writer) (string, error) {
	makensis, err := makensisPath(opts.Makensis)
	if err != nil {
		return "", err
	}
	binary, err := buildProject(dir, "windows", opts.Arch, goRun, stdout, stderr)
	if err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp("", "goi-pack-*")
	if err != nil {
		return "", err
	}
	if !opts.Keep {
		defer os.RemoveAll(stage)
	}
	if err := copyFile(binary, filepath.Join(stage, cfg.Name+".exe"), 0o755); err != nil {
		return "", err
	}
	if sdl != "" {
		real, err := filepath.EvalSymlinks(sdl)
		if err != nil {
			return "", err
		}
		// Next to the executable, which is where Windows looks for a DLL.
		if err := copyFile(real, filepath.Join(stage, packSDLFile("windows")), 0o644); err != nil {
			return "", err
		}
		if err := writeSDLLicense(filepath.Join(stage, "SDL3-LICENSE.txt")); err != nil {
			return "", err
		}
	}
	icoName := cfg.ID + ".ico"
	if err := writeICO(icon, filepath.Join(stage, icoName)); err != nil {
		return "", fmt.Errorf("icon: %w", err)
	}
	out := filepath.Join(dir, "dist", "windows-"+opts.Arch, cfg.Name+"-"+opts.Arch+"-setup.exe")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	// makensis resolves OutFile against the script's directory (the temp dir),
	// so the installer path in the script has to be absolute.
	absOut, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	script := filepath.Join(stage, "installer.nsi")
	if err := os.WriteFile(script, []byte(nsisScript(cfg, stage, absOut, icoName, sdl != "")), 0o644); err != nil {
		return "", err
	}
	if opts.Keep {
		fmt.Fprintln(stdout, "goi: appdir kept at", stage)
	}
	if err := runTool(makensis, []string{script}, os.Environ(), stdout, stderr); err != nil {
		return "", fmt.Errorf("makensis: %w", err)
	}
	return out, nil
}

// nsisScript is a per-user installer: it installs below %LOCALAPPDATA%, so no
// administrator prompt, and registers an uninstaller in Add or Remove Programs.
func nsisScript(cfg config, stage, out, icoName string, withSDL bool) string {
	file := func(name string) string { return filepath.ToSlash(filepath.Join(stage, name)) }
	var b strings.Builder
	fmt.Fprintf(&b, "Unicode true\n")
	fmt.Fprintf(&b, "Name \"%s\"\n", cfg.Name)
	fmt.Fprintf(&b, "OutFile \"%s\"\n", filepath.ToSlash(out))
	fmt.Fprintf(&b, "InstallDir \"$LOCALAPPDATA\\Programs\\%s\"\n", cfg.Name)
	fmt.Fprintf(&b, "InstallDirRegKey HKCU \"Software\\%s\" \"InstallDir\"\n", cfg.ID)
	b.WriteString("RequestExecutionLevel user\n")
	fmt.Fprintf(&b, "Icon \"%s\"\n", file(icoName))
	fmt.Fprintf(&b, "UninstallIcon \"%s\"\n", file(icoName))
	b.WriteString("Page directory\nPage instfiles\nUninstPage uninstConfirm\nUninstPage instfiles\n\n")

	b.WriteString("Section \"Install\"\n")
	b.WriteString("  SetOutPath \"$INSTDIR\"\n")
	fmt.Fprintf(&b, "  File \"%s\"\n", file(cfg.Name+".exe"))
	fmt.Fprintf(&b, "  File \"%s\"\n", file(icoName))
	if withSDL {
		fmt.Fprintf(&b, "  File \"%s\"\n", file(packSDLFile("windows")))
		fmt.Fprintf(&b, "  File \"%s\"\n", file("SDL3-LICENSE.txt"))
	}
	b.WriteString("  WriteUninstaller \"$INSTDIR\\uninstall.exe\"\n")
	fmt.Fprintf(&b, "  CreateShortcut \"$DESKTOP\\%s.lnk\" \"$INSTDIR\\%s.exe\" \"\" \"$INSTDIR\\%s\" 0\n", cfg.Name, cfg.Name, icoName)
	fmt.Fprintf(&b, "  CreateDirectory \"$SMPROGRAMS\\%s\"\n", cfg.Name)
	fmt.Fprintf(&b, "  CreateShortcut \"$SMPROGRAMS\\%s\\%s.lnk\" \"$INSTDIR\\%s.exe\" \"\" \"$INSTDIR\\%s\" 0\n", cfg.Name, cfg.Name, cfg.Name, icoName)
	fmt.Fprintf(&b, "  CreateShortcut \"$SMPROGRAMS\\%s\\Uninstall %s.lnk\" \"$INSTDIR\\uninstall.exe\"\n", cfg.Name, cfg.Name)
	fmt.Fprintf(&b, "  WriteRegStr HKCU \"Software\\%s\" \"InstallDir\" \"$INSTDIR\"\n", cfg.ID)
	uninstall := "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\" + cfg.ID
	fmt.Fprintf(&b, "  WriteRegStr HKCU \"%s\" \"DisplayName\" \"%s\"\n", uninstall, cfg.Name)
	fmt.Fprintf(&b, "  WriteRegStr HKCU \"%s\" \"DisplayIcon\" \"$INSTDIR\\%s\"\n", uninstall, icoName)
	fmt.Fprintf(&b, "  WriteRegStr HKCU \"%s\" \"UninstallString\" \"$\\\"$INSTDIR\\uninstall.exe$\\\"\"\n", uninstall)
	fmt.Fprintf(&b, "  WriteRegDWORD HKCU \"%s\" \"NoModify\" 1\n", uninstall)
	fmt.Fprintf(&b, "  WriteRegDWORD HKCU \"%s\" \"NoRepair\" 1\n", uninstall)
	b.WriteString("SectionEnd\n\n")

	b.WriteString("Section \"Uninstall\"\n")
	fmt.Fprintf(&b, "  Delete \"$INSTDIR\\%s.exe\"\n", cfg.Name)
	fmt.Fprintf(&b, "  Delete \"$INSTDIR\\%s\"\n", icoName)
	if withSDL {
		fmt.Fprintf(&b, "  Delete \"$INSTDIR\\%s\"\n", packSDLFile("windows"))
		b.WriteString("  Delete \"$INSTDIR\\SDL3-LICENSE.txt\"\n")
	}
	b.WriteString("  Delete \"$INSTDIR\\uninstall.exe\"\n")
	b.WriteString("  RMDir \"$INSTDIR\"\n")
	fmt.Fprintf(&b, "  Delete \"$DESKTOP\\%s.lnk\"\n", cfg.Name)
	fmt.Fprintf(&b, "  Delete \"$SMPROGRAMS\\%s\\%s.lnk\"\n", cfg.Name, cfg.Name)
	fmt.Fprintf(&b, "  Delete \"$SMPROGRAMS\\%s\\Uninstall %s.lnk\"\n", cfg.Name, cfg.Name)
	fmt.Fprintf(&b, "  RMDir \"$SMPROGRAMS\\%s\"\n", cfg.Name)
	fmt.Fprintf(&b, "  DeleteRegKey HKCU \"Software\\%s\"\n", cfg.ID)
	fmt.Fprintf(&b, "  DeleteRegKey HKCU \"%s\"\n", uninstall)
	b.WriteString("SectionEnd\n")
	return b.String()
}

// icoSizes are the representations Windows uses: menus, Explorer, and the large
// tile in the installer.
var icoSizes = []int{16, 32, 48, 64, 128, 256}

// writeICO converts the PNG icon into an .ico with uncompressed 32-bit
// bitmaps, which every NSIS version and Windows release reads.
func writeICO(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	d, err := img.Decode(b)
	if err != nil {
		return err
	}
	entries := make([][]byte, len(icoSizes))
	for i, size := range icoSizes {
		entries[i] = icoBitmap(img.Scale(d, size, size))
	}
	var out bytes.Buffer
	write := func(v any) { _ = binary.Write(&out, binary.LittleEndian, v) }
	write(uint16(0)) // reserved
	write(uint16(1)) // icon
	write(uint16(len(entries)))
	offset := 6 + 16*len(entries)
	for i, data := range entries {
		size := icoSizes[i]
		side := byte(size)
		if size >= 256 {
			// 0 means 256 in an ICO directory entry.
			side = 0
		}
		out.Write([]byte{side, side, 0, 0})
		write(uint16(1))  // color planes
		write(uint16(32)) // bits per pixel
		write(uint32(len(data)))
		write(uint32(offset))
		offset += len(data)
	}
	for _, data := range entries {
		out.Write(data)
	}
	return os.WriteFile(dst, out.Bytes(), 0o644)
}

// icoBitmap is one ICO image: a BITMAPINFOHEADER, the bottom-up BGRA pixels and
// the 1-bit transparency mask.
func icoBitmap(d *img.Data) []byte {
	n := d.W
	var b bytes.Buffer
	write := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	write(uint32(40))        // header size
	write(int32(n))          // width
	write(int32(n * 2))      // height: XOR image plus mask
	write(uint16(1))         // planes
	write(uint16(32))        // bits per pixel
	write(uint32(0))         // compression
	write(uint32(n * n * 4)) // image size
	write(int32(0))          // x pixels per meter
	write(int32(0))          // y pixels per meter
	write(uint32(0))         // colors used
	write(uint32(0))         // important colors
	for y := n - 1; y >= 0; y-- {
		row := d.Pix[y*n*4 : (y+1)*n*4]
		for x := 0; x < n; x++ {
			b.Write([]byte{row[x*4+2], row[x*4+1], row[x*4], row[x*4+3]})
		}
	}
	maskRow := ((n + 31) / 32) * 4
	mask := make([]byte, maskRow)
	for y := n - 1; y >= 0; y-- {
		clear(mask)
		for x := 0; x < n; x++ {
			if d.Pix[(y*n+x)*4+3] == 0 {
				mask[x/8] |= 0x80 >> (x % 8)
			}
		}
		b.Write(mask)
	}
	return b.Bytes()
}
