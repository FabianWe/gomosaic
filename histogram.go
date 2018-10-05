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

	// don't do anything for empty images
	if bounds.Empty() {
		return
	}

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

// GenHistogramFromList generates a histogram containing an entry for each image
// in the images list.
// k is the number of sub-divisons. If normalize is true the normalized
// histogram will be computed instead of the frequency histogram.
func GenHistogramFromList(k uint, normalize bool, images ...image.Image) *Histogram {
	res := NewHistogram(k)
	// we add the size of all images from the list to improve normalization
	size := 0
	for _, img := range images {
		bounds := img.Bounds()
		if bounds.Empty() {
			continue
		}
		res.Add(img, k)
		size += (bounds.Dx() * bounds.Dy())
	}
	if normalize {
		res = res.Normalize(size)
	}
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

// CreateHistograms creates histograms for all images in the ids list and loads
// the images through the given storage.
// If you want to create all histograms for a given storage you can use
// CreateAllHistograms as a shortcut.
// It runs the creation of histograms concurrently (how many go routines run
// concurrently can be controlled by numRoutines).
// k is the number of sub-divisons as described in the histogram type,
// If normalized is true the normalized histograms are computed.
// progress is a function that is called to inform about the progress,
// see doucmentation for ProgressFunc.
func CreateHistograms(ids []ImageID, storage ImageStorage, normalize bool, k uint, numRoutines int, progress ProgressFunc) ([]*Histogram, error) {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	numImages := len(ids)
	// any error that occurs sets this variable (first error)
	// this is done later
	var err error

	// struct that we use for the channel
	type job struct {
		pos int
		id  ImageID
	}

	res := make([]*Histogram, numImages)
	jobs := make(chan job, BufferSize)
	errorChan := make(chan error, BufferSize)
	for w := 0; w < numRoutines; w++ {
		go func() {
			for next := range jobs {
				image, imageErr := storage.LoadImage(next.id)
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
				res[next.pos] = hist
				errorChan <- nil
			}
		}()
	}

	go func() {
		for i, id := range ids {
			jobs <- job{pos: i, id: id}
		}
		close(jobs)
	}()

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

// CreateAllHistograms creates all histograms for images in the storage.
// It is a shortcut using CreateHistograms, see this documentation for details.
func CreateAllHistograms(storage ImageStorage, normalize bool, k uint, numRoutines int, progress ProgressFunc) ([]*Histogram, error) {
	return CreateHistograms(IDList(storage), storage, normalize, k, numRoutines, progress)
}

// CreateHistogramsSequential works as CreateAllHistograms but does not use
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
