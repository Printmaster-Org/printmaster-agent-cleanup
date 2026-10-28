//go:build windows && amd64

package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsExecutableContainsPrinterIcon(t *testing.T) {
	group, err := windows.FindResource(0, windows.ResourceID(1), windows.RT_GROUP_ICON)
	if err != nil {
		t.Fatalf("Windows executable is missing its shell/taskbar icon: %v", err)
	}
	data, err := windows.LoadResourceData(0, group)
	if err != nil {
		t.Fatal(err)
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	if len(data) != 6+14*len(sizes) || binary.LittleEndian.Uint16(data[2:]) != 1 ||
		int(binary.LittleEndian.Uint16(data[4:])) != len(sizes) {
		t.Fatalf("unexpected icon directory: %x", data)
	}
	for index, size := range sizes {
		entry := data[6+14*index:]
		id := binary.LittleEndian.Uint16(entry[12:])
		resource, err := windows.FindResource(0, windows.ResourceID(id), windows.RT_ICON)
		if err != nil {
			t.Fatal(err)
		}
		pixels, err := windows.LoadResourceData(0, resource)
		if err != nil {
			t.Fatal(err)
		}
		icon, err := png.Decode(bytes.NewReader(pixels))
		if err != nil {
			t.Fatal(err)
		}
		if icon.Bounds().Dx() != size || icon.Bounds().Dy() != size {
			t.Fatalf("icon size %d is missing", size)
		}
		_, _, _, alpha := icon.At(size/2, size/2).RGBA()
		if alpha == 0 {
			t.Fatalf("icon size %d is blank", size)
		}
	}
}
