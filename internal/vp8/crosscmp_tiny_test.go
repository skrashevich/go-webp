package vp8

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"testing"

	refvp8 "golang.org/x/image/vp8"
)

func TestCrossTiny(t *testing.T) {
	// Generate gradient 32x32.
	gradPath := "/tmp/grad32.png"
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			v := uint8((x + y) * 255 / 62)
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	f, _ := os.Create(gradPath)
	png.Encode(f, img)
	f.Close()

	webpPath := "/tmp/grad32_q75.webp"
	cmd := exec.Command("ffmpeg", "-y", "-i", gradPath,
		"-c:v", "libwebp", "-quality", "75", webpPath)
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(webpPath)
	chunkSize := int(data[16]) | int(data[17])<<8 | int(data[18])<<16 | int(data[19])<<24
	vp8data := data[20 : 20+chunkSize]

	// Parse header.
	fh, fpo, _ := parseFrameHeader(vp8data)
	bd, _ := newBoolDecoder(vp8data, fpo)
	cp := defaultCoeffProbs
	ph, _ := parseCompressedHeader(bd, fh, &cp)
	fmt.Printf("32x32 q=75: seg=%v absVal=%v qDelta=%v numParts=%d\n",
		ph.segment.enabled, ph.segment.absoluteValues, ph.segment.quantDelta, ph.numParts)

	// Reference decode.
	dec := refvp8.NewDecoder()
	dec.Init(bytes.NewReader(vp8data), len(vp8data))
	dec.DecodeFrameHeader()
	refImgYCbCr, _ := dec.DecodeFrame()

	// Our decode.
	ownImg, err := Decode(vp8data)
	if err != nil {
		t.Fatal(err)
	}
	ownYCbCr := ownImg.(*image.YCbCr)

	bounds := refImgYCbCr.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Print full Y comparison.
	fmt.Println("\nRef Y:")
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fmt.Printf("%3d ", refImgYCbCr.Y[(y+bounds.Min.Y)*refImgYCbCr.YStride+x+bounds.Min.X])
		}
		fmt.Println()
	}
	fmt.Println("\nOwn Y:")
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fmt.Printf("%3d ", ownYCbCr.Y[y*ownYCbCr.YStride+x])
		}
		fmt.Println()
	}
	fmt.Println("\nDiff:")
	totalDiff := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			rY := int(refImgYCbCr.Y[(y+bounds.Min.Y)*refImgYCbCr.YStride+x+bounds.Min.X])
			oY := int(ownYCbCr.Y[y*ownYCbCr.YStride+x])
			d := oY - rY
			fmt.Printf("%+3d ", d)
			if d < 0 {
				d = -d
			}
			totalDiff += d
		}
		fmt.Println()
	}
	fmt.Printf("\nTotal abs diff: %d, avg: %.2f\n", totalDiff, float64(totalDiff)/float64(w*h))
}
