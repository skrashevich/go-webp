package vp8

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"testing"

	refvp8 "golang.org/x/image/vp8"
)

func TestCrossNoFilter(t *testing.T) {
	srcPath := "/Users/svk/Downloads/tmpmkii6kk7.png"

	for _, size := range []int{64, 128, 256} {
		webpPath := fmt.Sprintf("/tmp/crossnf_%d.webp", size)
		cmd := exec.Command("ffmpeg", "-y", "-i", srcPath, "-vf", fmt.Sprintf("scale=%d:%d", size, size),
			"-c:v", "libwebp", "-quality", "75", webpPath)
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}

		data, _ := os.ReadFile(webpPath)
		chunkSize := int(data[16]) | int(data[17])<<8 | int(data[18])<<16 | int(data[19])<<24
		vp8data := data[20 : 20+chunkSize]

		// Reference decode (with filter).
		dec := refvp8.NewDecoder()
		dec.Init(bytes.NewReader(vp8data), len(vp8data))
		dec.DecodeFrameHeader()
		refImg, _ := dec.DecodeFrame()

		// Own decode WITHOUT filter.
		dNoF := &decoder{data: vp8data}
		fh, fpo, _ := parseFrameHeader(vp8data)
		dNoF.fh = fh
		bd, _ := newBoolDecoder(vp8data, fpo)
		dNoF.coeffProbs = defaultCoeffProbs
		ph, _ := parseCompressedHeader(bd, fh, &dNoF.coeffProbs)
		dNoF.ph = ph
		dNoF.buildQuantTables()
		dNoF.locatePartitions(fpo)
		dNoF.frame = newFrame(int(fh.width), int(fh.height))
		dNoF.decodeMacroblocks()
		ownNoFilter := dNoF.frame.toYCbCr()

		// Own decode WITH filter.
		ownImg, _ := Decode(vp8data)
		ownFiltered := ownImg.(*image.YCbCr)

		w, h := int(fh.width), int(fh.height)
		b := refImg.Bounds()

		psnrNF := psnrY(refImg, b, ownNoFilter, w, h)
		psnrF := psnrY(refImg, b, ownFiltered, w, h)

		fmt.Printf("%dx%d: ref_vs_own(noFilter)=%.1f dB, ref_vs_own(filtered)=%.1f dB, filter_level=%d\n",
			size, size, psnrNF, psnrF, ph.filter.level)
	}
}

func psnrY(ref *image.YCbCr, b image.Rectangle, own *image.YCbCr, w, h int) float64 {
	var sumSq float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			rY := float64(ref.Y[(y+b.Min.Y)*ref.YStride+x+b.Min.X])
			oY := float64(own.Y[y*own.YStride+x])
			d := rY - oY
			sumSq += d * d
		}
	}
	mse := sumSq / float64(w*h)
	if mse == 0 {
		return 999.0
	}
	return 10 * math.Log10(255*255/mse)
}
