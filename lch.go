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
	"fmt"
	"image"
	"sync"

	log "github.com/sirupsen/logrus"
)

// LCH is a sorted collection of global color histograms. Different schemes
// yield to a different number of LCHs, but for each image the same number of
// GCHs is computed.
//
// All histograms should be generated with the same k (sub-divisons).
type LCH struct {
	Histograms []*Histogram
}

func NewLCH(histograms []*Histogram) *LCH {
	return &LCH{Histograms: histograms}
}

// RepairDistribution is used to ensure that distribution contains a matrix
// of numY rows and in each row numX columns. Usually this method does not do
// anything (and hopefully never will). But just to be sure we add it here.
// It will never decrease the number of rectangles, only increase if required.
//
// This function is usally only triggered in debug mode.
func RepairDistribution(distribution [][]image.Rectangle, numX, numY int) [][]image.Rectangle {
	y := len(distribution)
	if y != numY {
		log.WithFields(log.Fields{
			"expected": numY,
			"got":      y,
		}).Warn("FixedNumDivider returned distribution with wrong number of tiles (height)")
	}
	for j := y; j < numY; j++ {
		distribution = append(distribution, make([]image.Rectangle, numX))
	}
	for j := 0; j < numY; j++ {
		rects := distribution[j]
		x := len(rects)
		if x != numX {
			log.WithFields(log.Fields{
				"expected": numX,
				"got":      x,
				"row":      j,
			}).Warn("FixedNumDivider returned distribution with wrong number of tiles (width)")
		}
		for i := x; i < numX; i++ {
			rects = append(rects, image.Rectangle{})
		}
		distribution[j] = rects
	}
	return distribution
}

// LCHScheme is used to compute a LCH from an image (k is the number of
// sub-divisons for histogram generation).
//
// Examples of such schemes: Four parts north, west, south and east or
// five parts north, west, south, east and center.
type LCHScheme interface {
	ComputLCH(img image.Image, k uint) (*LCH, error)
}

// FourLCHScheme implements the scheme with four parts: north, west, south and
// east.
//
// It implements LCHScheme, the LCH contains the GCHs for the parts in the order
// described above.
type FourLCHScheme struct{}

// NewFourLCHScheme returns a new FourLCHScheme.
func NewFourLCHScheme() FourLCHScheme {
	return FourLCHScheme{}
}

// ComputLCH returns exactly for histograms (N, W, S, E).
func (s FourLCHScheme) ComputLCH(img image.Image, k uint) (*LCH, error) {
	res := make([]*Histogram, 4)
	// first distribute image into 4 blocks
	// setting cut to false means that these blocks are not necessarily of the
	// same size.
	divider := NewFixedNumDivider(2, 2, false)
	parts := divider.Divide(img)
	if Debug {
		// if in debug mode check for errors while dividing the image
		parts = RepairDistribution(parts, 2, 2)
	}
	imageParts, partsErr := DivideImage(img, parts, 4)
	if partsErr != nil {
		return nil, fmt.Errorf("Error computing distribution for LCH: %v", partsErr)
	}

	var dist [][]image.Image = [][]image.Image{
		// north
		[]image.Image{imageParts[0][0], imageParts[0][1]},
		// west
		[]image.Image{imageParts[0][0], imageParts[1][0]},
		// south
		[]image.Image{imageParts[1][0], imageParts[1][1]},
		// east
		[]image.Image{imageParts[0][1], imageParts[1][1]},
	}
	// for each part compute GCH
	var wg sync.WaitGroup
	wg.Add(4)
	for i, imgList := range dist {
		go func(index int, list []image.Image) {
			defer wg.Done()
			// compute histogram from image list
			res[index] = GenHistogramFromList(k, true, list...)
		}(i, imgList)
	}
	wg.Wait()
	return NewLCH(res), nil
}
