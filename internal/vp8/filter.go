package vp8

// filter2 modifies a 2-pixel wide or 2-pixel high band along an edge.
// Ported from golang.org/x/image/vp8/filter.go.
func filter2(pix []byte, level, index, iStep, jStep int) {
	for n := 16; n > 0; n, index = n-1, index+iStep {
		p1 := int(pix[index-2*jStep])
		p0 := int(pix[index-1*jStep])
		q0 := int(pix[index+0*jStep])
		q1 := int(pix[index+1*jStep])
		if abs(p0-q0)<<1+abs(p1-q1)>>1 > level {
			continue
		}
		a := 3*(q0-p0) + clamp127(p1-q1)
		a1 := clamp15((a + 4) >> 3)
		a2 := clamp15((a + 3) >> 3)
		pix[index-1*jStep] = clamp255(p0 + a2)
		pix[index+0*jStep] = clamp255(q0 - a1)
	}
}

// filter246 modifies a 2-, 4- or 6-pixel wide or high band along an edge.
func filter246(pix []byte, n, level, ilevel, hlevel, index, iStep, jStep int, fourNotSix bool) {
	for ; n > 0; n, index = n-1, index+iStep {
		p3 := int(pix[index-4*jStep])
		p2 := int(pix[index-3*jStep])
		p1 := int(pix[index-2*jStep])
		p0 := int(pix[index-1*jStep])
		q0 := int(pix[index+0*jStep])
		q1 := int(pix[index+1*jStep])
		q2 := int(pix[index+2*jStep])
		q3 := int(pix[index+3*jStep])
		if abs(p0-q0)<<1+abs(p1-q1)>>1 > level {
			continue
		}
		if abs(p3-p2) > ilevel ||
			abs(p2-p1) > ilevel ||
			abs(p1-p0) > ilevel ||
			abs(q1-q0) > ilevel ||
			abs(q2-q1) > ilevel ||
			abs(q3-q2) > ilevel {
			continue
		}
		if abs(p1-p0) > hlevel || abs(q1-q0) > hlevel {
			// Filter 2 pixels.
			a := 3*(q0-p0) + clamp127(p1-q1)
			a1 := clamp15((a + 4) >> 3)
			a2 := clamp15((a + 3) >> 3)
			pix[index-1*jStep] = clamp255(p0 + a2)
			pix[index+0*jStep] = clamp255(q0 - a1)
		} else if fourNotSix {
			// Filter 4 pixels.
			a := 3 * (q0 - p0)
			a1 := clamp15((a + 4) >> 3)
			a2 := clamp15((a + 3) >> 3)
			a3 := (a1 + 1) >> 1
			pix[index-2*jStep] = clamp255(p1 + a3)
			pix[index-1*jStep] = clamp255(p0 + a2)
			pix[index+0*jStep] = clamp255(q0 - a1)
			pix[index+1*jStep] = clamp255(q1 - a3)
		} else {
			// Filter 6 pixels.
			a := clamp127(3*(q0-p0) + clamp127(p1-q1))
			a1 := (27*a + 63) >> 7
			a2 := (18*a + 63) >> 7
			a3 := (9*a + 63) >> 7
			pix[index-3*jStep] = clamp255(p2 + a3)
			pix[index-2*jStep] = clamp255(p1 + a2)
			pix[index-1*jStep] = clamp255(p0 + a1)
			pix[index+0*jStep] = clamp255(q0 - a1)
			pix[index+1*jStep] = clamp255(q1 - a2)
			pix[index+2*jStep] = clamp255(q2 - a3)
		}
	}
}

// filterParam holds the loop filter parameters for a macroblock.
type filterParam struct {
	level, ilevel, hlevel uint8
	inner                 bool
}

// computeFilterParamsTable precomputes filter params for all 4 segments × 2 modes,
// matching the reference golang.org/x/image/vp8 computeFilterParams.
// mode index: 0 = Y16 (usePredY16=true), 1 = B_PRED (usePredY16=false).
func (d *decoder) computeFilterParamsTable() [4][2]filterParam {
	ph := d.ph
	lf := &ph.filter
	var table [4][2]filterParam

	for seg := 0; seg < 4; seg++ {
		baseLevel := lf.level
		if ph.segment.enabled && ph.segment.updateData {
			segFilterDelta := int(ph.segment.filterDelta[seg])
			if ph.segment.absoluteValues {
				baseLevel = segFilterDelta
			} else {
				baseLevel += segFilterDelta
			}
		}

		for mode := 0; mode < 2; mode++ {
			p := &table[seg][mode]
			// inner = true for B_PRED (mode=1), false for Y16 (mode=0).
			// This matches reference: p.inner = j != 0
			p.inner = mode != 0

			level := baseLevel
			if lf.deltasEnabled {
				// refLFDelta[0] for intra frames.
				level += int(lf.refDelta[0])
				if mode != 0 {
					// modeLFDelta[0] for B_PRED mode.
					level += int(lf.modeDelta[0])
				}
			}

			if level <= 0 {
				p.level = 0
				continue
			}
			if level > 63 {
				level = 63
			}

			ilevel := level
			if lf.sharpness > 0 {
				if lf.sharpness > 4 {
					ilevel >>= 2
				} else {
					ilevel >>= 1
				}
				if x := 9 - lf.sharpness; ilevel > x {
					ilevel = x
				}
			}
			if ilevel < 1 {
				ilevel = 1
			}

			p.ilevel = uint8(ilevel)
			p.level = uint8(2*level + ilevel)

			if ph.frame.keyFrame {
				if level < 15 {
					p.hlevel = 0
				} else if level < 40 {
					p.hlevel = 1
				} else {
					p.hlevel = 2
				}
			} else {
				if level < 15 {
					p.hlevel = 0
				} else if level < 20 {
					p.hlevel = 1
				} else if level < 40 {
					p.hlevel = 2
				} else {
					p.hlevel = 3
				}
			}
		}
	}
	return table
}

// applySimpleFilter applies the simple loop filter to the frame.
func (d *decoder) applySimpleFilter() {
	f := d.frame
	mbW := d.ph.mbWidth
	mbH := d.ph.mbHeight

	for mby := 0; mby < mbH; mby++ {
		for mbx := 0; mbx < mbW; mbx++ {
			fp := d.perMBFilterParams[mby*mbW+mbx]
			if fp.level == 0 {
				continue
			}
			l := int(fp.level)
			yIndex := (mby*f.yStride + mbx) * 16

			if mbx > 0 {
				filter2(f.y, l+4, yIndex, f.yStride, 1)
			}
			if fp.inner {
				filter2(f.y, l, yIndex+4, f.yStride, 1)
				filter2(f.y, l, yIndex+8, f.yStride, 1)
				filter2(f.y, l, yIndex+12, f.yStride, 1)
			}
			if mby > 0 {
				filter2(f.y, l+4, yIndex, 1, f.yStride)
			}
			if fp.inner {
				filter2(f.y, l, yIndex+f.yStride*4, 1, f.yStride)
				filter2(f.y, l, yIndex+f.yStride*8, 1, f.yStride)
				filter2(f.y, l, yIndex+f.yStride*12, 1, f.yStride)
			}
		}
	}
}

// applyNormalFilter applies the normal loop filter to the frame.
func (d *decoder) applyNormalFilter() {
	f := d.frame
	mbW := d.ph.mbWidth
	mbH := d.ph.mbHeight

	for mby := 0; mby < mbH; mby++ {
		for mbx := 0; mbx < mbW; mbx++ {
			fp := d.perMBFilterParams[mby*mbW+mbx]
			if fp.level == 0 {
				continue
			}
			l := int(fp.level)
			il := int(fp.ilevel)
			hl := int(fp.hlevel)
			yIndex := (mby*f.yStride + mbx) * 16
			cIndex := (mby*f.cStride + mbx) * 8

			if mbx > 0 {
				filter246(f.y, 16, l+4, il, hl, yIndex, f.yStride, 1, false)
				filter246(f.cb, 8, l+4, il, hl, cIndex, f.cStride, 1, false)
				filter246(f.cr, 8, l+4, il, hl, cIndex, f.cStride, 1, false)
			}
			if fp.inner {
				filter246(f.y, 16, l, il, hl, yIndex+4, f.yStride, 1, true)
				filter246(f.y, 16, l, il, hl, yIndex+8, f.yStride, 1, true)
				filter246(f.y, 16, l, il, hl, yIndex+12, f.yStride, 1, true)
				filter246(f.cb, 8, l, il, hl, cIndex+4, f.cStride, 1, true)
				filter246(f.cr, 8, l, il, hl, cIndex+4, f.cStride, 1, true)
			}
			if mby > 0 {
				filter246(f.y, 16, l+4, il, hl, yIndex, 1, f.yStride, false)
				filter246(f.cb, 8, l+4, il, hl, cIndex, 1, f.cStride, false)
				filter246(f.cr, 8, l+4, il, hl, cIndex, 1, f.cStride, false)
			}
			if fp.inner {
				filter246(f.y, 16, l, il, hl, yIndex+f.yStride*4, 1, f.yStride, true)
				filter246(f.y, 16, l, il, hl, yIndex+f.yStride*8, 1, f.yStride, true)
				filter246(f.y, 16, l, il, hl, yIndex+f.yStride*12, 1, f.yStride, true)
				filter246(f.cb, 8, l, il, hl, cIndex+f.cStride*4, 1, f.cStride, true)
				filter246(f.cr, 8, l, il, hl, cIndex+f.cStride*4, 1, f.cStride, true)
			}
		}
	}
}

// applyLoopFilter applies the loop filter after all macroblocks are decoded.
// If loop_filter_level == 0, this is a no-op.
func (d *decoder) applyLoopFilter() {
	if d.ph.filter.level == 0 {
		return
	}
	if d.ph.filter.filterType == 1 {
		d.applySimpleFilter()
	} else {
		d.applyNormalFilter()
	}
}

// Helper math functions used by the filter.

const intSize = 32 << (^uint(0) >> 63)

func abs(x int) int {
	m := x >> (intSize - 1)
	return (x ^ m) - m
}

func clamp15(x int) int {
	if x < -16 {
		return -16
	}
	if x > 15 {
		return 15
	}
	return x
}

func clamp127(x int) int {
	if x < -128 {
		return -128
	}
	if x > 127 {
		return 127
	}
	return x
}

func clamp255(x int) byte {
	if x < 0 {
		return 0
	}
	if x > 255 {
		return 255
	}
	return byte(x)
}
