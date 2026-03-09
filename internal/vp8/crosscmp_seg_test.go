package vp8

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"testing"

	refvp8 "golang.org/x/image/vp8"
)

func TestCrossSegmentation(t *testing.T) {
	// Generate a simple gradient PNG.
	gradPath := "/tmp/grad_seg.png"
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			v := uint8((x + y) * 255 / 254)
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	f, _ := os.Create(gradPath)
	png.Encode(f, img)
	f.Close()

	for _, q := range []int{50, 60, 70, 75, 80, 90} {
		webpPath := fmt.Sprintf("/tmp/seg_%d.webp", q)
		cmd := exec.Command("ffmpeg", "-y", "-i", gradPath,
			"-c:v", "libwebp", "-quality", fmt.Sprintf("%d", q), webpPath)
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}

		data, _ := os.ReadFile(webpPath)
		chunkSize := int(data[16]) | int(data[17])<<8 | int(data[18])<<16 | int(data[19])<<24
		vp8data := data[20 : 20+chunkSize]

		// Parse header to check segmentation.
		fh, fpo, _ := parseFrameHeader(vp8data)
		bd, _ := newBoolDecoder(vp8data, fpo)
		cp := defaultCoeffProbs
		ph, _ := parseCompressedHeader(bd, fh, &cp)

		// Reference decode.
		dec := refvp8.NewDecoder()
		dec.Init(bytes.NewReader(vp8data), len(vp8data))
		dec.DecodeFrameHeader()
		refImgYCbCr, _ := dec.DecodeFrame()

		// Our decode.
		ownImg, _ := Decode(vp8data)
		ownYCbCr := ownImg.(*image.YCbCr)

		bounds := refImgYCbCr.Bounds()
		w, h := bounds.Dx(), bounds.Dy()
		var sumSq float64
		maxErr := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				rY := int(refImgYCbCr.Y[(y+bounds.Min.Y)*refImgYCbCr.YStride+x+bounds.Min.X])
				oY := int(ownYCbCr.Y[y*ownYCbCr.YStride+x])
				d := rY - oY
				sumSq += float64(d * d)
				if d < 0 {
					d = -d
				}
				if d > maxErr {
					maxErr = d
				}
			}
		}
		mse := sumSq / float64(w*h)
		psnr := 999.0
		if mse > 0 {
			psnr = 10 * math.Log10(255*255/mse)
		}
		fmt.Printf("q=%d: PSNR=%.1f dB, maxErr=%d, seg=%v absVal=%v qDelta=%v\n",
			q, psnr, maxErr, ph.segment.enabled, ph.segment.absoluteValues, ph.segment.quantDelta)
	}
}
