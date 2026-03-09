package vp8

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"testing"

	refvp8 "golang.org/x/image/vp8"
)

// TestCrossCompareReal decodes a real ffmpeg-encoded WebP with both
// our decoder and the reference, comparing Y-plane values per 4x4 block.
func TestCrossCompareReal(t *testing.T) {
	srcPath := "/Users/svk/Downloads/tmpmkii6kk7.png"
	webpPath := "/tmp/crosscmp.webp"

	cmd := exec.Command("ffmpeg", "-y", "-i", srcPath, "-vf", "scale=128:128",
		"-c:v", "libwebp", "-quality", "75", webpPath)
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(webpPath)
	chunkSize := int(data[16]) | int(data[17])<<8 | int(data[18])<<16 | int(data[19])<<24
	vp8data := data[20 : 20+chunkSize]

	// Reference decode.
	dec := refvp8.NewDecoder()
	dec.Init(bytes.NewReader(vp8data), len(vp8data))
	_, err := dec.DecodeFrameHeader()
	if err != nil {
		t.Fatal("ref DecodeFrameHeader:", err)
	}
	refImg, err := dec.DecodeFrame()
	if err != nil {
		t.Fatal("ref DecodeFrame:", err)
	}

	// Our decode.
	ownImg, err := Decode(vp8data)
	if err != nil {
		t.Fatal("own decode:", err)
	}
	ownYCbCr := ownImg.(*image.YCbCr)

	// Compare Y plane, block by block.
	b := refImg.Bounds()
	w, h := b.Dx(), b.Dy()
	fmt.Printf("Image: %dx%d, ref bounds: %v\n", w, h, b)

	firstErr := true
	totalDiff := 0
	for mby := 0; mby < (h+15)/16; mby++ {
		for mbx := 0; mbx < (w+15)/16; mbx++ {
			// Compare all 16 4x4 blocks in this MB.
			for by := 0; by < 4; by++ {
				for bx := 0; bx < 4; bx++ {
					maxDiff := 0
					for py := 0; py < 4; py++ {
						for px := 0; px < 4; px++ {
							x := mbx*16 + bx*4 + px
							y := mby*16 + by*4 + py
							if x >= w || y >= h {
								continue
							}
							refY := refImg.Y[(y+b.Min.Y)*refImg.YStride+x+b.Min.X]
							ownY := ownYCbCr.Y[y*ownYCbCr.YStride+x]
							d := int(ownY) - int(refY)
							if d < 0 {
								d = -d
							}
							if d > maxDiff {
								maxDiff = d
							}
							totalDiff += d
						}
					}
					if maxDiff > 2 && firstErr {
						x0 := mbx*16 + bx*4
						y0 := mby*16 + by*4
						fmt.Printf("\nFirst divergence at MB(%d,%d) blk(%d,%d) pos=(%d,%d):\n",
							mbx, mby, bx, by, x0, y0)
						for py := 0; py < 4; py++ {
							x := mbx*16 + bx*4
							y := mby*16 + by*4 + py
							if y >= h {
								break
							}
							fmt.Printf("  y=%d: ref=[", y)
							for px := 0; px < 4; px++ {
								if x+px >= w {
									break
								}
								refY := refImg.Y[(y+b.Min.Y)*refImg.YStride+x+px+b.Min.X]
								fmt.Printf("%3d ", refY)
							}
							fmt.Printf("] own=[")
							for px := 0; px < 4; px++ {
								if x+px >= w {
									break
								}
								ownY := ownYCbCr.Y[y*ownYCbCr.YStride+x+px]
								fmt.Printf("%3d ", ownY)
							}
							fmt.Printf("] diff=[")
							for px := 0; px < 4; px++ {
								if x+px >= w {
									break
								}
								refY := refImg.Y[(y+b.Min.Y)*refImg.YStride+x+px+b.Min.X]
								ownY := ownYCbCr.Y[y*ownYCbCr.YStride+x+px]
								fmt.Printf("%+4d ", int(ownY)-int(refY))
							}
							fmt.Println("]")
						}
						firstErr = false
					}
				}
			}
		}
	}

	avgDiff := float64(totalDiff) / float64(w*h)
	fmt.Printf("\nTotal Y diff: %d, avg per pixel: %.2f\n", totalDiff, avgDiff)

	// Per-MB max diff summary.
	fmt.Println("\nPer-MB max Y diff:")
	for mby := 0; mby < (h+15)/16; mby++ {
		for mbx := 0; mbx < (w+15)/16; mbx++ {
			maxMBDiff := 0
			for py := 0; py < 16; py++ {
				for px := 0; px < 16; px++ {
					x := mbx*16 + px
					y := mby*16 + py
					if x >= w || y >= h {
						continue
					}
					refY := refImg.Y[(y+b.Min.Y)*refImg.YStride+x+b.Min.X]
					ownY := ownYCbCr.Y[y*ownYCbCr.YStride+x]
					d := int(ownY) - int(refY)
					if d < 0 {
						d = -d
					}
					if d > maxMBDiff {
						maxMBDiff = d
					}
				}
			}
			fmt.Printf("%3d ", maxMBDiff)
		}
		fmt.Println()
	}

	// Also print first few MB Y values for MB(0,0).
	fmt.Println("\nMB(0,0) ref Y top-left 8x8:")
	for y := 0; y < 8 && y < h; y++ {
		fmt.Printf("  ")
		for x := 0; x < 8 && x < w; x++ {
			fmt.Printf("%3d ", refImg.Y[(y+b.Min.Y)*refImg.YStride+x+b.Min.X])
		}
		fmt.Println()
	}
	fmt.Println("MB(0,0) own Y top-left 8x8:")
	for y := 0; y < 8 && y < h; y++ {
		fmt.Printf("  ")
		for x := 0; x < 8 && x < w; x++ {
			fmt.Printf("%3d ", ownYCbCr.Y[y*ownYCbCr.YStride+x])
		}
		fmt.Println()
	}
}
