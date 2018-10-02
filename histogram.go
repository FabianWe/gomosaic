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
	"strings"
)

// Histogram describes a color histogram for an image.
// It counts the number of pixels with a certain color or the relative frequency
// of the color (normalized histogram).
//
// An entry for color r, g, b quantized to k sub-divisions is stored at position
// r + k * g + k * k * b.
//
// To compute the id of an r, g, b color use RGBID or ID on RGB objects.
type Histogram struct {
	// Entries contains for each r, g, b color the frequency. The histogram does
	// not save each possible r, g, b color but the quantizd version.
	// That is it stores frequencies (r, g, b) where r, g, b < k.
	Entries []float64
	// K is the number of sub-divisions used to create the histogram.
	// It must be a number between 1 and 256.
	K uint
}

// NewHistogram creates a new histogram given the number of sub-divions in each
// direction. k must be a number between 1 and 256.
func NewHistogram(k uint) *Histogram {
	return &Histogram{make([]float64, k*k*k), k}
}

// String returns a tuple representation of the histogram.
func (h *Histogram) String() string {
	strs := make([]string, len(h.Entries))
	for i, entry := range h.Entries {
		strs[i] = fmt.Sprintf("%.2f", entry)
	}
	return "〈" + strings.Join(strs, ", ") + "〉"
}

// PrintInfo prints information about the histogram to the standard output.
// If verbose is true it prints a formatted table of all frequencies, otherwise
// it prints the shorter tuple representation.
func (h *Histogram) PrintInfo(verbose bool) {
	numCategories := h.K * h.K * h.K
	fmt.Printf("Histogram consisting of k = %d sub-divisions, leading to %d color categories\n", h.K, numCategories)
	if verbose {
		fmt.Printf("%-6s %6s %6s %10s\n", "red", "green", "blue", "value")
		var r, g, b uint
		for ; r < h.K; r++ {
			g = 0
			for ; g < h.K; g++ {
				b = 0
				for ; b < h.K; b++ {
					fmt.Printf("%6d %6d %6d %10.2f\n", r, g, b, h.Entries[RGBID(r, g, b, h.K)])
				}
			}
		}
	} else {
		fmt.Println(h)
	}
}

// Equals  checks if two histograms are equal. epsilon is the difference
// between that is allowed to still consider them equal.
func (h *Histogram) Equals(other *Histogram, epsilon float64) bool {
	if h.K != other.K {
		return false
	}
	for i, e1 := range h.Entries {
		e2 := other.Entries[i]
		if math.Abs(e1-e2) > epsilon {
			return false
		}
	}
	return true
}

// Add creates the histogram given an image, that is it counts how often
// a color appears in the image. k is the number of sub-divisions in each
// direction, it must be a number between 1 and 256.
// The histogram contains the freuqency of each color after quantiation in
// k sub-divisions.
//
// This method can be called multiple times to accumulate the histograms of
// multiple image,s it is however not save to concurrently call this method
// on the same histogram.
//
// To create a histogram for one image you can also use GenHistogram.
func (h *Histogram) Add(img image.Image, k uint) {
	bounds := img.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// get generic color
			c := img.At(x, y)
			// convert to internal rgb representation
			rgb := ConvertRGB(c)
			// quantize to k divisions
			rgb = rgb.Quantize(k)
			// update result entry
			h.Entries[rgb.ID(k)]++
		}
	}
}

// GenHistogram creates a histogram given an image and the number of sub-divions
// in each direction (k), k must be a number between 1 and 256.
// The histogram contains the freuqency of each color after quantiation in
// k sub-divisions.
func GenHistogram(img image.Image, k uint) *Histogram {
	res := NewHistogram(k)
	res.Add(img, k)
	return res
}

// EntrySum returns the sum of all entries in the histogram.
func (h *Histogram) EntrySum() float64 {
	var res float64
	for _, entry := range h.Entries {
		res += entry
	}
	return res
}

// Normalize computes the normalized histogram of h if h contains the number
// of occurrences in the image.
// pixels is the total number of pixels in the original image the historam was
// created for. If pixels is a negative number or 0 the number of pixels will be
// computed as the sum of all entries in the original histogram.
// If no pixels exist in the image all result entries are set to 0.
func (h *Histogram) Normalize(pixels int) *Histogram {
	var size float64
	if pixels > 0 {
		size = float64(pixels)
	} else {
		// sum all entries
		size = h.EntrySum()
	}
	res := NewHistogram(h.K)
	// testing 0.0 should be okay.
	if size == 0.0 {
		return res
	}
	for i, entry := range h.Entries {
		res.Entries[i] = entry / size
	}
	return res
}

// CreateHistograms creates histograms for all images in the given storage.
// It runs the creation of histograms concurrently (how many go routines run
// concurrently can be controlled by numRoutines).
// k is the number of sub-divisons as described in the histogram type,
// If normalized is true the normalized histograms are computed.
// progress is a function that is called to inform about the progress,
// see doucmentation for ProgressFunc.
func CreateHistograms(storage ImageStorage, normalize bool, k uint, numRoutines int, progress ProgressFunc) ([]*Histogram, error) {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	numImages := storage.NumImages()
	// any error that occurs sets this variable (first error)
	// this is done later
	var err error
	res := make([]*Histogram, numImages)
	jobs := make(chan ImageID, 1000)
	errorChan := make(chan error, 1000)
	for w := 0; w < numRoutines; w++ {
		go func(worker int) {
			for next := range jobs {
				image, imageErr := storage.LoadImage(next)
				if imageErr != nil {
					errorChan <- imageErr
					continue
				}
				hist := GenHistogram(image, k)
				if normalize {
					bounds := image.Bounds()
					if !bounds.Empty() {
						size := bounds.Dx() * bounds.Dy()
						hist = hist.Normalize(size)
					}
				}
				res[next] = hist
				errorChan <- nil
			}
		}(w)
	}

	go func() {
		var job ImageID
		for ; job < numImages; job++ {
			jobs <- job
		}
		close(jobs)
	}()

	var i ImageID
	for ; i < numImages; i++ {
		nextErr := <-errorChan
		if nextErr != nil && err == nil {
			err = nextErr
		}
		if progress != nil {
			progress(int(i))
		}
	}
	return res, err
}

// CreateHistogramsSequential works as CreateHistograms but does not use
// concurrency.
func CreateHistogramsSequential(storage ImageStorage, normalize bool, k uint, progress ProgressFunc) ([]*Histogram, error) {
	numImages := storage.NumImages()
	res := make([]*Histogram, numImages)
	var i ImageID
	for ; i < numImages; i++ {
		image, imageErr := storage.LoadImage(i)
		if imageErr != nil {
			return nil, imageErr
		}
		hist := GenHistogram(image, k)
		if normalize {
			bounds := image.Bounds()
			if !bounds.Empty() {
				size := bounds.Dx() * bounds.Dy()
				hist = hist.Normalize(size)
			}
		}
		res[i] = hist
		if progress != nil {
			progress(int(i))
		}
	}
	return res, nil
}
