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
	"math"
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

// NewLCH creates a new LCH givent the histograms.
func NewLCH(histograms []*Histogram) *LCH {
	return &LCH{Histograms: histograms}
}

// Dist returns the distance between two LCHs parameterized by a HistogramMetric
// two compare the histograms. It returns
// |Δ(h1[1], h2[1])| + ... + |Δ(h1[n], h2[n])| if n is the number of GCHs
// of the LCH.
//
// If the LCHs are of different dimensions or the GCHs inside the LCHs are
// of different dimensions an error != nil is returned.
// TODO test if paralellism makes sense here... or is it just too fast to
// help? But I would prefer "concurrency first"
func (lch *LCH) Dist(other *LCH, delta HistogramMetric) (float64, error) {
	if len(lch.Histograms) != len(other.Histograms) {
		return -1.0, fmt.Errorf("Invalid LCH dimensions: %d != %d",
			len(lch.Histograms),
			len(other.Histograms))
	}
	sum := 0.0
	for i, h1 := range lch.Histograms {
		h2 := other.Histograms[i]
		if len(h1.Entries) != len(h2.Entries) {
			return -1.0, fmt.Errorf("Invalid histogram dimensions (in LCH): %d != %d",
				len(h1.Entries),
				len(h2.Entries))
		}
		sum += math.Abs(delta(h1, h2))
	}
	return sum, nil
}

// RepairDistribution is used to ensure that distribution contains a matrix
// of numY rows and in each row numX columns. Usually this method does not do
// anything (and hopefully never will). But just to be sure we add it here.
// It will never decrease the number of rectangles, only increase if required.
//
// This function is usally only triggered in debug mode.
func RepairDistribution(distribution TileDivision, numX, numY int) TileDivision {
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

// LCHScheme returns the distribution of an image into sub images.
// Note that a sub image can be contained in multiple lists and not all lists
// must be of the same length. For example the four parts scheme: The first
// list could contain both top sub images. The western list would contain the
// bot left sub images. They both contain the to-left image.
//
// Schemes always return a fixed number of image lists.
type LCHScheme interface {
	GetParts(img image.Image) ([][]image.Image, error)
}

// GenLCH computes the LCHs an image. It uses the scheme to compute the image
// parts and then concurrently creates the GCHs for each list.
// k and normalize are defined as for the GCH method: k is the number of
// histogram sub-divisions and if normalize is true the GCHs are normalized.
func GenLCH(scheme LCHScheme, img image.Image, k uint, normalize bool) (*LCH, error) {
	dist, distErr := scheme.GetParts(img)
	if distErr != nil {
		return nil, distErr
	}
	res := make([]*Histogram, len(dist))
	// for each part compute GCH
	var wg sync.WaitGroup
	wg.Add(len(dist))
	for i, imgList := range dist {
		go func(index int, list []image.Image) {
			defer wg.Done()
			// compute histogram from image list
			res[index] = GenHistogramFromList(k, normalize, list...)
		}(i, imgList)
	}
	wg.Wait()
	return NewLCH(res), nil
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

// GetParts returns exactly four histograms (N, W, S, E).
func (s FourLCHScheme) GetParts(img image.Image) ([][]image.Image, error) {
	// first divide image into 4 blocks
	// setting cut to false means that these blocks are not necessarily of the
	// same size.
	divider := NewFixedNumDivider(2, 2, false)
	parts := divider.Divide(img.Bounds())
	if Debug {
		// if in debug mode check for errors while dividing the image
		parts = RepairDistribution(parts, 2, 2)
	}
	imageParts, partsErr := DivideImage(img, parts, 4)
	if partsErr != nil {
		return nil, fmt.Errorf("Error computing distribution for LCH: %s", partsErr.Error())
	}
	res := [][]image.Image{
		// north
		[]image.Image{imageParts[0][0], imageParts[0][1]},
		// west
		[]image.Image{imageParts[0][0], imageParts[1][0]},
		// south
		[]image.Image{imageParts[1][0], imageParts[1][1]},
		// east
		[]image.Image{imageParts[0][1], imageParts[1][1]},
	}
	return res, nil
}

// FiveLCHScheme implements the scheme with vie parts: north, west, south,
// east and center.
//
// It implements LCHScheme, the LCH contains the GCHs for the parts in the order
// described above.
type FiveLCHScheme struct{}

// NewFiveLCHScheme returns a new FourLCHScheme.
func NewFiveLCHScheme() FiveLCHScheme {
	return FiveLCHScheme{}
}

// GetParts returns exactly five histograms (N, W, S, E, C).
func (s FiveLCHScheme) GetParts(img image.Image) ([][]image.Image, error) {
	// first divide image into 9 blocks
	// setting cut to false means that these blocks are not necessarily of the
	// same size.
	divider := NewFixedNumDivider(3, 3, false)
	parts := divider.Divide(img.Bounds())
	if Debug {
		// if in debug mode check for errors while dividing the image
		parts = RepairDistribution(parts, 3, 3)
	}
	imageParts, partsErr := DivideImage(img, parts, 9)
	if partsErr != nil {
		return nil, fmt.Errorf("Error computing distribution for LCH: %s", partsErr.Error())
	}
	res := [][]image.Image{
		// north
		[]image.Image{imageParts[0][0], imageParts[0][1], imageParts[0][2]},
		// west
		[]image.Image{imageParts[0][0], imageParts[1][0], imageParts[2][0]},
		// south
		[]image.Image{imageParts[2][0], imageParts[2][1], imageParts[2][2]},
		// east
		[]image.Image{imageParts[0][2], imageParts[1][2], imageParts[2][2]},
		// center
		[]image.Image{imageParts[1][1]},
	}
	return res, nil
}

func CreateLCHs(scheme LCHScheme, ids []ImageID, storage ImageStorage, normalize bool,
	k uint, numRoutines int, progress ProgressFunc) ([]*LCH, error) {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	numImages := len(ids)
	// any error that occurs sets this variable (first error)
	// this is done later
	var err error

	res := make([]*LCH, numImages)
	jobs := make(chan int, BufferSize)
	errorChan := make(chan error, BufferSize)

	// workers
	for w := 0; w < numRoutines; w++ {
		go func() {
			for next := range jobs {
				image, imageErr := storage.LoadImage(ids[next])
				if imageErr != nil {
					errorChan <- imageErr
					continue
				}
				lch, lchErr := GenLCH(scheme, image, k, normalize)
				if lchErr != nil {
					errorChan <- lchErr
					continue
				}
				res[next] = lch
				errorChan <- nil
			}
		}()
	}

	// create jobs
	go func() {
		for i := 0; i < len(ids); i++ {
			jobs <- i
		}
		close(jobs)
	}()

	// read errors
	for i := 0; i < numImages; i++ {
		nextErr := <-errorChan
		if nextErr != nil && err == nil {
			err = nextErr
		}
		if progress != nil {
			progress(i)
		}
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CreateAllLCHs creates all lchs for images in the storage.
// It is a shortcut using CreateLCHs, see this documentation for details.
func CreateAllLCHs(scheme LCHScheme, storage ImageStorage, normalize bool,
	k uint, numRoutines int, progress ProgressFunc) ([]*LCH, error) {
	return CreateLCHs(scheme, IDList(storage), storage, normalize, k, numRoutines, progress)
}

// LCHStorage maps image ids to LCHs.
// By default the histograms of the LCHs should be normalized.
//
// Implementations must be safe for concurrent use.
type LCHStorage interface {
	// GetLCH returns the LCH for a previously registered ImageID.
	GetLCH(id ImageID) (*LCH, error)
}
