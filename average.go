// Copyright 2018 Fabian Wenzelmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gomosaic

import (
	"image"
)

// AverageColor descibes the average of several RGB colors.
type AverageColor RGB

// ComputeAverageColor computes the average color of an image.
func ComputeAverageColor(img image.Image) AverageColor {
	// just to be sure we use big integers, depending on the image size we might
	// get problems

	bounds := img.Bounds()

	// don't do anything for empty images
	if bounds.Empty() {
		return AverageColor{}
	}
	var r, g, b uint64
	numPixels := uint64(bounds.Dx() * bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// get generic color
			c := img.At(x, y)
			// convert to internal rgb representation
			rgb := ConvertRGB(c)
			r += uint64(rgb.R)
			g += uint64(rgb.G)
			b += uint64(rgb.B)
		}
	}
	r /= numPixels
	g /= numPixels
	b /= numPixels
	return AverageColor{R: uint8(r), G: uint8(g), B: uint8(b)}
}

// Dist returns the distance between the two average color vectors given the
// metric for the component vectors.
func (c AverageColor) Dist(other AverageColor, metric VectorMetric) float64 {
	v1 := []float64{float64(c.R), float64(c.G), float64(c.B)}
	v2 := []float64{float64(other.R), float64(other.G), float64(other.B)}
	return metric(v1, v2)
}
