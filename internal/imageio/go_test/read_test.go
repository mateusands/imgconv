// The read path's contract: what a file CLAIMS about itself is never trusted.
//
// Two different claims, two different defences. The extension claims a format,
// and is ignored entirely — the bytes decide, because a .png holding JPEG is a
// normal thing to find on a real disk. The header claims dimensions, and is
// checked before anything is allocated, because that claim is the cheapest lie
// an attacker can tell: a few hundred bytes that ask for gigabytes.
//
// The near-limit test is the one that matters most and is easiest to leave out.
// A bound that rejects everything passes the far-over case exactly like a correct
// one; only a file just under the limit tells the two apart.
package go_test

import (
	"bytes"
	"encoding/binary"
	. "github.com/mateusands/imgconv/internal/imageio"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

func TestDecode_ShouldReportJpegWhenTheFileIsNamedPngButHoldsJpegBytes(t *testing.T) {
	dir := t.TempDir()
	// Named .png on purpose. If the extension were consulted anywhere in the
	// read path, this is the test that catches it.
	path := filepath.Join(dir, "lying-extension.png")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	jpegFormat, ok := Lookup("jpeg")
	if !ok {
		t.Fatal("jpeg is not in the registry")
	}
	if err := jpegFormat.Encode(f, testImage(t), Options{}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	img, format, err := src.Decode(Limits{})
	if err != nil {
		t.Fatalf("the file should decode by its bytes: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("format = %q, want \"jpeg\" — the extension was trusted", format)
	}
	if img.Bounds().Dx() != 8 || img.Bounds().Dy() != 8 {
		t.Errorf("bounds = %v, want 8x8", img.Bounds())
	}
}

// pngHeaderClaiming builds a PNG signature and IHDR that DECLARE the given size,
// with no pixel data behind them. That is exactly the shape of a decompression
// bomb: the cost is in what the header asks the decoder to allocate, not in the
// bytes on disk. This file is 45 bytes.
func pngHeaderClaiming(w, h uint32) []byte {
	var ihdr bytes.Buffer
	binary.Write(&ihdr, binary.BigEndian, w)
	binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0}) // 8-bit RGBA, no compression/filter/interlace

	chunk := append([]byte("IHDR"), ihdr.Bytes()...)

	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	binary.Write(&out, binary.BigEndian, uint32(len(ihdr.Bytes())))
	out.Write(chunk)
	binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return out.Bytes()
}

func TestDecode_ShouldRefuseBeforeDecodingWhenTheHeaderDeclaresTooManyPixels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bomb.png")
	bomb := pngHeaderClaiming(60000, 60000) // 3.6 G pixels, ~14 GB decoded
	if err := os.WriteFile(path, bomb, 0o644); err != nil {
		t.Fatal(err)
	}
	if len(bomb) > 1000 {
		t.Fatalf("the fixture should be tiny to make the point, got %d bytes", len(bomb))
	}

	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	_, _, err = src.Decode(Limits{})
	if err == nil {
		t.Fatal("expected a refusal before decoding, got nil")
	}
	// Asserting on the SPECIFIC error, not on any error: this file is also
	// truncated, so a test happy with any failure would pass without the limit
	// ever running.
	if !bytes.Contains([]byte(err.Error()), []byte("above the limit")) {
		t.Errorf("error = %q, want the pixel-limit refusal", err)
	}
}

func TestDecode_ShouldAcceptAnImageJustUnderTheLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	png, _ := Lookup("png")
	if err := png.Encode(f, testImage(t), Options{}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// The image is 8x8 = 64 pixels. 64 is allowed; 63 is not.
	if _, _, err := src.Decode(Limits{MaxPixels: 64}); err != nil {
		t.Errorf("exactly at the limit should be allowed: %v", err)
	}
	if _, _, err := src.Decode(Limits{MaxPixels: 63}); err == nil {
		t.Error("one pixel over the limit should be refused")
	}
}

func TestLimits_ShouldRejectAMaxPixelsAboveTheCeiling(t *testing.T) {
	if err := (Limits{MaxPixels: MaxAllowedPixels + 1}).Validate(); err == nil {
		t.Error("a limit above the ceiling should be a usage error — an unbounded knob is not a limit")
	}
	if err := (Limits{MaxPixels: -1}).Validate(); err == nil {
		t.Error("a negative limit should be a usage error")
	}
	if err := (Limits{MaxPixels: DefaultMaxPixels}).Validate(); err != nil {
		t.Errorf("the default should validate: %v", err)
	}
}

func TestWrite_ShouldSayDecodeOnlyWhenTheTargetFormatHasNoEncoder(t *testing.T) {
	webp, ok := Lookup("webp")
	if !ok {
		t.Fatal("webp should be in the registry as a readable format")
	}
	if webp.CanEncode() {
		t.Fatal("webp is decode-only: x/image ships no encoder for it")
	}

	dir := t.TempDir()
	err := Write(filepath.Join(dir, "out.webp"), nil, testImage(t), webp, Options{}, false)
	if err == nil {
		t.Fatal("expected a refusal for a decode-only target")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("decode only")) {
		t.Errorf("error = %q, want it to say the format is decode only", err)
	}
}

func TestOpen_ShouldRefuseADirectory(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Error("opening a directory as an image should fail")
	}
}

// BMP is in the registry because golang.org/x/image already carries an encoder
// AND a decoder for it, and that module is already a dependency. It cost one row
// and no new vendor, which is the only reason it is here: the format list is not
// a race to be long, it is a list of what this build can actually do.
func TestRegistry_ShouldSupportBmpBothWays(t *testing.T) {
	bmp, ok := Lookup("bmp")
	if !ok {
		t.Fatal("bmp is not in the registry")
	}
	if !bmp.CanEncode() {
		t.Error("x/image ships a BMP encoder, so bmp must be offerable as a target")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "saida.bmp")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := bmp.Encode(f, testImage(t), Options{}); err != nil {
		t.Fatalf("encoding bmp: %v", err)
	}
	f.Close()

	// The round trip is what proves the row is wired to a real codec rather than
	// to a name: written by us, and read back by the sniffer as bmp.
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	format, err := src.Sniff()
	if err != nil {
		t.Fatalf("sniffing what we just wrote: %v", err)
	}
	if format != "bmp" {
		t.Errorf("format = %q, want bmp", format)
	}
}

func TestByExtension_ShouldAnswerToTheBmpExtension(t *testing.T) {
	f, ok := ByExtension(".bmp")
	if !ok || f.Name != "bmp" {
		t.Errorf("ByExtension(\".bmp\") = %+v, %v — the extension guess must reach the new row", f, ok)
	}
}

// Header exists so a caller can describe a file without decoding it.
//
// WHY IT MATTERS HERE: the graphical front end lists whatever the operator picked
// and wants to show its dimensions. Doing that with Decode would allocate every
// pixel of every file in the list just to print two numbers — on a picker, which
// is the one place a decode bomb would be handed to the program by accident.
// DecodeConfig reads the header and stops.
func TestHeader_ShouldReportFormatAndDimensionsWithoutDecoding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mentiroso.txt") // PNG bytes, non-image name
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	png, ok := Lookup("png")
	if !ok {
		t.Fatal("png is not in the registry")
	}
	if err := png.Encode(f, testImage(t), Options{}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	h, err := src.Header()
	if err != nil {
		t.Fatalf("reading the header: %v", err)
	}
	if h.Format != "png" {
		t.Errorf("Format = %q, want png — the name was trusted over the bytes", h.Format)
	}
	// testImage is 8x8; the helper is the contract for that number.
	if h.Width != 8 || h.Height != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", h.Width, h.Height)
	}
}

// The header of a bomb must be readable without paying for the bomb. This is the
// same 60000x60000 fixture the decode guard refuses, and Header answers about it
// instead of refusing, because describing a file is not decoding it.
func TestHeader_ShouldDescribeAFileTooLargeToDecode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bomba.png")
	if err := os.WriteFile(path, pngHeaderClaiming(60000, 60000), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	h, err := src.Header()
	if err != nil {
		t.Fatalf("the header is 45 bytes and must be readable: %v", err)
	}
	if h.Width != 60000 || h.Height != 60000 {
		t.Errorf("dimensions = %dx%d, want 60000x60000", h.Width, h.Height)
	}
	// And decoding it is still refused, which is the point of the split.
	if _, _, err := src.Decode(Limits{}); err == nil {
		t.Error("describing a bomb must not make it decodable")
	}
}

func TestHeader_ShouldFailForBytesThatAreNotAnImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notas.txt")
	if err := os.WriteFile(path, []byte("apenas texto"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	if _, err := src.Header(); err == nil {
		t.Error("text is not an image and describing it must say so")
	}
}
