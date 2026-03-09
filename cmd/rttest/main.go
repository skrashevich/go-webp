package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"

	"github.com/skrashevich/go-webp"
)

func main() {
	f, _ := os.Open("/Users/svk/Downloads/tmpmkii6kk7.png")
	origImg, _ := png.Decode(f)
	f.Close()

	// Encode with go-webp lossy.
	var buf bytes.Buffer
	err := webp.Encode(&buf, origImg, &webp.Options{Lossy: true, Quality: 75})
	if err != nil {
		fmt.Println("Encode error:", err)
		return
	}
	fmt.Printf("Encoded size: %d bytes\n", buf.Len())

	// Save to file for ffmpeg testing.
	os.WriteFile("/tmp/gowebp_lossy_q75.webp", buf.Bytes(), 0644)

	// Decode with go-webp.
	decodedImg, _, err := image.Decode(&buf)
	if err != nil {
		fmt.Println("Decode error:", err)
		return
	}

	// Compute PSNR.
	bounds := origImg.Bounds()
	w := bounds.Max.X - bounds.Min.X
	h := bounds.Max.Y - bounds.Min.Y
	var mse float64
	n := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r1, g1, b1, _ := origImg.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			r2, g2, b2, _ := decodedImg.At(x, y).RGBA()
			dr := float64(r1>>8) - float64(r2>>8)
			dg := float64(g1>>8) - float64(g2>>8)
			db := float64(b1>>8) - float64(b2>>8)
			mse += dr*dr + dg*dg + db*db
			n += 3
		}
	}
	mse /= float64(n)
	if mse == 0 {
		fmt.Println("PSNR: inf (lossless)")
	} else {
		psnr := 10 * math.Log10(255*255/mse)
		fmt.Printf("Own decoder PSNR: %.2f dB\n", psnr)
	}
	fmt.Printf("Image: %dx%d, Decoded: %dx%d\n", w, h, decodedImg.Bounds().Dx(), decodedImg.Bounds().Dy())
}
