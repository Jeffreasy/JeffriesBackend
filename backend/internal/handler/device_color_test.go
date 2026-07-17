package handler

import "testing"

func TestHSVToRGBClampsInputsAndOutputs(t *testing.T) {
	cases := [][3]float64{{-1, -2, -3}, {2, 4, 3}, {0.5, 1, 1}}
	for _, input := range cases {
		r, g, b := hsvToRGB(input[0], input[1], input[2])
		for _, channel := range []int{r, g, b} {
			if channel < 0 || channel > 255 {
				t.Fatalf("HSV %v produced out-of-range RGB (%d,%d,%d)", input, r, g, b)
			}
		}
	}
}
