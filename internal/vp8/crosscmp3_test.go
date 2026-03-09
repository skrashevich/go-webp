package vp8

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// TestCrossCoeffs decodes MB(0,0) of 128x128 real photo, printing
// per-block coefficient state to find exactly where things go wrong.
func TestCrossCoeffs(t *testing.T) {
	srcPath := "/Users/svk/Downloads/tmpmkii6kk7.png"
	webpPath := "/tmp/crosscoeffs.webp"

	cmd := exec.Command("ffmpeg", "-y", "-i", srcPath, "-vf", "scale=128:128",
		"-c:v", "libwebp", "-quality", "75", webpPath)
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(webpPath)
	chunkSize := int(data[16]) | int(data[17])<<8 | int(data[18])<<16 | int(data[19])<<24
	vp8data := data[20 : 20+chunkSize]

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

	// Re-parse header for actual decoding.
	headerBD, _ := newBoolDecoder(vp8data, firstPartOffset)
	var dummy [4][8][3][11]uint8
	parseCompressedHeader(headerBD, fh, &dummy)

	mbW := ph.mbWidth
	d.upNZMask = make([]uint8, mbW)
	d.upNZY2 = make([]uint8, mbW)
	d.upPred = make([][4]uint8, mbW)

	dctBD := d.parts[0]

	// Manually decode MB(0,0) step by step.
	seg := 0
	if ph.segment.enabled && ph.segment.updateMap {
		if !headerBD.ReadBool(ph.segment.prob[0]) {
			if headerBD.ReadBool(ph.segment.prob[1]) {
				seg = 1
			}
		} else {
			if headerBD.ReadBool(ph.segment.prob[2]) {
				seg = 3
			} else {
				seg = 2
			}
		}
	}
	q := d.segQuant[seg]
	fmt.Printf("MB(0,0) seg=%d q.yDCQ=%d q.yACQ=%d\n", seg, q.yDCQ, q.yACQ)

	skipCoeff := false
	if ph.useSkipProb {
		skipCoeff = headerBD.ReadBool(ph.skipProb)
	}
	fmt.Printf("skipCoeff=%v\n", skipCoeff)

	// Y16 or B_PRED?
	isY16 := headerBD.ReadBool(145)
	fmt.Printf("isY16=%v\n", isY16)

	if isY16 {
		t.Fatal("expected B_PRED for this test")
	}

	// Parse B_PRED modes.
	var mode4 [16]int
	d.leftPred = [4]uint8{}
	for j := 0; j < 4; j++ {
		p := d.leftPred[j]
		for i := 0; i < 4; i++ {
			above := d.upPred[0][i]
			prob := &predProb[above][p]
			var refMode uint8
			if !headerBD.ReadBool(prob[0]) {
				refMode = refPredDC
			} else if !headerBD.ReadBool(prob[1]) {
				refMode = refPredTM
			} else if !headerBD.ReadBool(prob[2]) {
				refMode = refPredVE
			} else if !headerBD.ReadBool(prob[3]) {
				if !headerBD.ReadBool(prob[4]) {
					refMode = refPredHE
				} else if !headerBD.ReadBool(prob[5]) {
					refMode = refPredRD
				} else {
					refMode = refPredVR
				}
			} else if !headerBD.ReadBool(prob[6]) {
				refMode = refPredLD
			} else if !headerBD.ReadBool(prob[7]) {
				refMode = refPredVL
			} else if !headerBD.ReadBool(prob[8]) {
				refMode = refPredHD
			} else {
				refMode = refPredHU
			}
			mode4[j*4+i] = refToOurMode[refMode]
			p = refMode
			d.upPred[0][i] = refMode
		}
		d.leftPred[j] = p
	}

	modeNames := map[int]string{
		predDC4: "DC", predV4: "VE", predH4: "HE", predTM4: "TM",
		predRD4: "RD", predVR4: "VR", predLD4: "LD", predVL4: "VL",
		predHD4: "HD", predHU4: "HU",
	}
	fmt.Printf("4x4 modes:\n")
	for j := 0; j < 4; j++ {
		fmt.Printf("  row %d: ", j)
		for i := 0; i < 4; i++ {
			fmt.Printf("%-4s", modeNames[mode4[j*4+i]])
		}
		fmt.Println()
	}

	// Parse chroma mode.
	{
		p := keyFrameUVModeProbs
		if !headerBD.ReadBool(p[0]) {
			fmt.Println("modeUV=DC")
		} else if !headerBD.ReadBool(p[1]) {
			fmt.Println("modeUV=V")
		} else if !headerBD.ReadBool(p[2]) {
			fmt.Println("modeUV=H")
		} else {
			fmt.Println("modeUV=TM")
		}
	}

	// Now decode Y coefficients block by block.
	yProbs := &d.coeffProbs[3] // Y1SansY2 for B_PRED
	var yCoeffs [16][16]int16
	lnz := unpackNZ(0) // leftNZMask = 0 for first MB
	unz := unpackNZ(0) // upNZMask = 0 for first MB

	for y := 0; y < 4; y++ {
		nz := lnz[y]
		for x := 0; x < 4; x++ {
			blk := y*4 + x
			fmt.Printf("\nBlock(%d,%d) mode=%s dctBD:pos=%d r=%d b=0x%08x n=%d ctx=%d\n",
				x, y, modeNames[mode4[blk]],
				dctBD.pos, dctBD.rangeM1, dctBD.bits, dctBD.nBits,
				nz+unz[x])
			nz = decodeResiduals4(dctBD, yProbs, nz+unz[x], q.yDCQ, q.yACQ, false, &yCoeffs[blk])
			unz[x] = nz

			// Print non-zero coefficients.
			hasNZ := false
			for i := 0; i < 16; i++ {
				if yCoeffs[blk][i] != 0 {
					if !hasNZ {
						fmt.Printf("  coeffs:")
						hasNZ = true
					}
					fmt.Printf(" [%d]=%d", i, yCoeffs[blk][i])
				}
			}
			if hasNZ {
				fmt.Println()
			} else {
				fmt.Println("  coeffs: all zero")
			}
			fmt.Printf("  nz=%d\n", nz)
		}
		lnz[y] = nz
	}
}
