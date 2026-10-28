//go:build ignore

// Generate the Windows executable icon from the shared SVG using the MinGW resource compiler.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, "generate Windows icon:", err)
		os.Exit(1)
	}
}

func generate() error {
	source, err := os.ReadFile(filepath.Join("..", "..", "internal", "ui", "icon.svg"))
	if err != nil {
		return err
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	header := make([]byte, 6+16*len(sizes))
	binary.LittleEndian.PutUint16(header[2:], 1)
	binary.LittleEndian.PutUint16(header[4:], uint16(len(sizes)))
	var images bytes.Buffer
	for index, size := range sizes {
		icon, err := oksvg.ReadIconStream(bytes.NewReader(source), oksvg.StrictErrorMode)
		if err != nil {
			return err
		}
		icon.SetTarget(0, 0, float64(size), float64(size))
		target := image.NewRGBA(image.Rect(0, 0, size, size))
		icon.Draw(rasterx.NewDasher(size, size, rasterx.NewScannerGV(size, size, target, target.Bounds())), 1)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, target); err != nil {
			return err
		}
		entry := header[6+16*index:]
		if size < 256 {
			entry[0], entry[1] = byte(size), byte(size)
		}
		binary.LittleEndian.PutUint16(entry[4:], 1)
		binary.LittleEndian.PutUint16(entry[6:], 32)
		binary.LittleEndian.PutUint32(entry[8:], uint32(encoded.Len()))
		binary.LittleEndian.PutUint32(entry[12:], uint32(len(header)+images.Len()))
		images.Write(encoded.Bytes())
	}
	directory, err := os.MkdirTemp("", "agent-cleanup-icon-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := os.WriteFile(filepath.Join(directory, "app.ico"), append(header, images.Bytes()...), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "app.rc"), []byte("1 ICON \"app.ico\"\n"), 0600); err != nil {
		return err
	}
	output, err := filepath.Abs("resource_windows_amd64.syso")
	if err != nil {
		return err
	}
	command := exec.Command("windres", "--target=pe-x86-64", "--input=app.rc", "--output-format=coff", "--output="+output)
	command.Dir = directory
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}
