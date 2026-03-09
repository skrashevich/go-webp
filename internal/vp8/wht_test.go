package vp8

import (
	"fmt"
	"testing"
)

func TestWHTRoundTrip(t *testing.T) {
	// Checker DC pattern: alternating -224 and 576
	var dc [16]int16
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if (x+y)%2 == 0 {
				dc[y*4+x] = -224
			} else {
				dc[y*4+x] = 576
			}
		}
	}
	fmt.Println("DC input:", dc)

	var whtOut [16]int16
	fwht4x4(&dc, &whtOut)
	fmt.Println("fWHT output:", whtOut)

	var reconDC [16]int16
	iWHT4x4(&whtOut, &reconDC)
	fmt.Println("iWHT roundtrip:", reconDC)

	// Check if roundtrip preserves values (within rounding)
	for i := 0; i < 16; i++ {
		diff := dc[i] - reconDC[i]
		if diff < 0 {
			diff = -diff
		}
		if diff > 2 {
			t.Errorf("position %d: input %d, output %d, diff %d", i, dc[i], reconDC[i], diff)
		}
	}
}
