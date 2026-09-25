// Package testgen builds synthetic Google Takeout exports for tests and CI.
// Every image is drawn here or comes from the tiny generated files in
// testdata; nothing comes from a camera or a real account.
package testgen

import (
	"bytes"
	"embed"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
)

//go:embed testdata/*
var fixtures embed.FS

// seedColor gives each seed its own color, so every generated file has
// different bytes and never deduplicates by accident.
func seedColor(seed int) color.RGBA {
	return color.RGBA{uint8(37 * seed), uint8(91*seed + 40), uint8(151*seed + 80), 255}
}

func tile(seed int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	c := seedColor(seed)
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, c)
		}
	}
	// A second color in one corner keeps nearby seeds distinct after JPEG rounding.
	img.Set(seed%16, (seed/16)%12, color.RGBA{255 - c.R, 255 - c.G, 255 - c.B, 255})
	return img
}

// JPEG returns a small JPEG with no metadata.
func JPEG(seed int) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, tile(seed), &jpeg.Options{Quality: 90}); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// JPEGWithDate returns a JPEG whose EXIF DateTimeOriginal is dto
// ("2006:01:02 15:04:05"), the way a camera writes it.
func JPEGWithDate(seed int, dto string) []byte {
	if len(dto) != 19 {
		panic("dto must be YYYY:MM:DD HH:MM:SS")
	}
	j := JPEG(seed)
	var t bytes.Buffer
	le := binary.LittleEndian
	t.WriteString("II*\x00")
	binary.Write(&t, le, uint32(8))
	// IFD0: one entry pointing at the Exif IFD.
	binary.Write(&t, le, uint16(1))
	binary.Write(&t, le, uint16(0x8769))
	binary.Write(&t, le, uint16(4))
	binary.Write(&t, le, uint32(1))
	binary.Write(&t, le, uint32(26))
	binary.Write(&t, le, uint32(0))
	// Exif IFD: DateTimeOriginal.
	binary.Write(&t, le, uint16(1))
	binary.Write(&t, le, uint16(0x9003))
	binary.Write(&t, le, uint16(2))
	binary.Write(&t, le, uint32(20))
	binary.Write(&t, le, uint32(44))
	binary.Write(&t, le, uint32(0))
	t.WriteString(dto + "\x00")
	payload := append([]byte("Exif\x00\x00"), t.Bytes()...)
	var app1 bytes.Buffer
	app1.Write([]byte{0xff, 0xe1})
	binary.Write(&app1, binary.BigEndian, uint16(len(payload)+2))
	app1.Write(payload)
	out := append([]byte{}, j[:2]...)
	out = append(out, app1.Bytes()...)
	return append(out, j[2:]...)
}

// PNG returns a small PNG with no metadata.
func PNG(seed int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, tile(seed)); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// GIF returns a small GIF.
func GIF(seed int) []byte {
	pal := color.Palette{color.Black, seedColor(seed)}
	img := image.NewPaletted(image.Rect(0, 0, 8, 8), pal)
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 2)
	}
	var b bytes.Buffer
	if err := gif.Encode(&b, img, nil); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// Fixture returns a committed file from testdata, for formats Go cannot encode.
// Files are named still{1,2,3}.heic|webp and clip{1,2,3}.mp4|mov|3gp|mkv|webm.
func Fixture(name string) []byte {
	b, err := fixtures.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return b
}

// Unique returns an ISO-BMFF file (HEIC, MP4, MOV, 3GP) with a trailing
// "free" box holding tag, so one fixture yields many distinct files. Readers
// skip free boxes.
func Unique(isoBMFF []byte, tag string) []byte {
	box := make([]byte, 8, 8+len(tag))
	binary.BigEndian.PutUint32(box, uint32(8+len(tag)))
	copy(box[4:], "free")
	box = append(box, tag...)
	return append(append([]byte{}, isoBMFF...), box...)
}

// TIFFRAW returns a minimal little-endian TIFF, the container DNG and most
// camera RAW formats use.
func TIFFRAW(seed int) []byte {
	var t bytes.Buffer
	le := binary.LittleEndian
	t.WriteString("II*\x00")
	binary.Write(&t, le, uint32(8))
	entries := [][3]uint32{
		{0x0100, 3, 1}, {0x0101, 3, 1}, {0x0102, 3, 8}, {0x0103, 3, 1},
		{0x0106, 3, 1}, {0x0111, 4, 0}, {0x0115, 3, 1}, {0x0117, 4, 1},
	}
	dataOff := uint32(8 + 2 + len(entries)*12 + 4)
	binary.Write(&t, le, uint16(len(entries)))
	for _, e := range entries {
		v := e[2]
		if e[0] == 0x0111 {
			v = dataOff
		}
		binary.Write(&t, le, uint16(e[0]))
		binary.Write(&t, le, uint16(e[1]))
		binary.Write(&t, le, uint32(1))
		binary.Write(&t, le, v)
	}
	binary.Write(&t, le, uint32(0))
	t.WriteByte(byte(seed))
	return t.Bytes()
}
