package vp8

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"testing"
)

// TestCrossDebug decodes the first MB of a 128x128 real photo, printing
// intermediate state to identify where divergence starts.
func TestCrossDebug(t *testing.T) {
	srcPath := "/Users/svk/Downloads/tmpmkii6kk7.png"
	webpPath := "/tmp/crossdbg.webp"

	// Use gradient instead of photo for cleaner analysis.
	gradPath := "/tmp/grad32_dbg.png"
	{
		import_img := image.NewRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				v := uint8((x + y) * 255 / 62)
				import_img.Set(x, y, color.RGBA{v, v, v, 255})
			}
		}
		f2, _ := os.Create(gradPath)
		png.Encode(f2, import_img)
		f2.Close()
	}
	srcPath = gradPath

	cmd := exec.Command("ffmpeg", "-y", "-i", srcPath,
		"-c:v", "libwebp", "-quality", "75", webpPath)
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(webpPath)
	chunkSize := int(data[16]) | int(data[17])<<8 | int(data[18])<<16 | int(data[19])<<24
	vp8data := data[20 : 20+chunkSize]

	// Parse headers.
	d := &decoder{data: vp8data}
	fh, firstPartOffset, _ := parseFrameHeader(vp8data)
	d.fh = fh

	bd, _ := newBoolDecoder(vp8data, firstPartOffset)
	d.coeffProbs = defaultCoeffProbs
	ph, _ := parseCompressedHeader(bd, fh, &d.coeffProbs)
	d.ph = ph

	d.buildQuantTables()
	d.locatePartitions(firstPartOffset)
	d.frame = newFrame(int(fh.width), int(fh.height))

	mbW := ph.mbWidth

	fmt.Printf("Frame: %dx%d, mbW=%d, numParts=%d\n", fh.width, fh.height, mbW, ph.numParts)
	fmt.Printf("Segments: enabled=%v updateMap=%v absoluteValues=%v quantDelta=%v\n",
		ph.segment.enabled, ph.segment.updateMap, ph.segment.absoluteValues, ph.segment.quantDelta)
	fmt.Printf("SegProbs: %v\n", ph.segment.prob)
	for i := 0; i < 4; i++ {
		fmt.Printf("SegQuant[%d]: yDCQ=%d yACQ=%d y2DCQ=%d y2ACQ=%d uvDCQ=%d uvACQ=%d\n",
			i, d.segQuant[i].yDCQ, d.segQuant[i].yACQ,
			d.segQuant[i].y2DCQ, d.segQuant[i].y2ACQ,
			d.segQuant[i].uvDCQ, d.segQuant[i].uvACQ)
	}

	// Now re-parse for actual decoding.
	headerBD, _ := newBoolDecoder(vp8data, firstPartOffset)
	var dummy [4][8][3][11]uint8
	parseCompressedHeader(headerBD, fh, &dummy)

	d.upNZMask = make([]uint8, mbW)
	d.upNZY2 = make([]uint8, mbW)
	d.upPred = make([][4]uint8, mbW)

	// Decode first row of MBs.
	partIdx := 0
	dctBD := d.parts[partIdx]
	var leftNZ, leftY2 uint8
	d.leftPred = [4]uint8{}

	for mbX := 0; mbX < mbW && mbX < 2; mbX++ {
		fmt.Printf("\n=== MB(%d,0) ===\n", mbX)
		fmt.Printf("headerBD: pos=%d rangeM1=%d bits=0x%08x nBits=%d\n",
			headerBD.pos, headerBD.rangeM1, headerBD.bits, headerBD.nBits)
		fmt.Printf("dctBD: pos=%d rangeM1=%d bits=0x%08x nBits=%d\n",
			dctBD.pos, dctBD.rangeM1, dctBD.bits, dctBD.nBits)

		// Peek at segment/mode.
		savedHdr := *headerBD
		seg := 0
		if ph.segment.enabled && ph.segment.updateMap {
			if !savedHdr.ReadBool(ph.segment.prob[0]) {
				if savedHdr.ReadBool(ph.segment.prob[1]) {
					seg = 1
				}
			} else {
				if savedHdr.ReadBool(ph.segment.prob[2]) {
					seg = 3
				} else {
					seg = 2
				}
			}
		}
		skip := false
		if ph.useSkipProb {
			skip = savedHdr.ReadBool(ph.skipProb)
		}
		isY16 := savedHdr.ReadBool(145)
		fmt.Printf("Peek: seg=%d skip=%v isY16=%v\n", seg, skip, isY16)

		newLeft, newY2, err := d.decodeMB(headerBD, dctBD, mbX, 0, leftNZ, d.upNZMask[mbX], leftY2, d.upNZY2[mbX], nil)
		if err != nil {
			t.Fatalf("MB(%d,0): %v", mbX, err)
		}
		leftNZ = newLeft
		leftY2 = newY2

		// Print Y values for this MB.
		yBase := mbX * 16
		fmt.Printf("Y values (first 8 rows):\n")
		for y := 0; y < 8; y++ {
			fmt.Printf("  y=%d: ", y)
			for x := 0; x < 16; x++ {
				fmt.Printf("%3d ", d.frame.y[y*d.frame.yStride+yBase+x])
			}
			fmt.Println()
		}
	}
}
