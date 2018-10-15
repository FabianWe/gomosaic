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
	"math/rand"
	"time"

	log "github.com/sirupsen/logrus"
)

func computeSingleHeap(storage ImageStorage, metric ImageMetric, i, j int, target *ImageHeap) error {
	numImages := storage.NumImages()
	var imageID ImageID
	for ; imageID < numImages; imageID++ {
		dist, distErr := metric.Compare(storage, imageID, i, j)
		if distErr != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: distErr,
				"image":      imageID,
				"tileY":      i,
				"tileX":      j,
			}).Error("Can't compute metric value, ignoreing it")
			continue
		}
		target.Add(imageID, dist)
	}
	return nil
}

// ComputeHeaps computes the image heap for each tile given k (the number of
// images to store in each heap).
//
// Metric will not be initialized, that must happen before.
func ComputeHeaps(storage ImageStorage, metric ImageMetric, query image.Image, dist TileDivision,
	k, numRoutines int, progress ProgressFunc) ([][]*ImageHeap, error) {
	// concurrently compute heaps
	// first, create all heapss
	heaps := make([][]*ImageHeap, len(dist))
	for i, col := range dist {
		size := len(col)
		heapsCol := make([]*ImageHeap, size)
		// initialize heap
		for j := 0; j < size; j++ {
			heapsCol[j] = NewImageHeap(k)
		}
		heaps[i] = heapsCol
	}

	type job struct {
		i, j int
	}

	jobs := make(chan job, BufferSize)
	errors := make(chan error, BufferSize)

	// set later
	var err error

	for w := 0; w < numRoutines; w++ {
		go func() {
			for next := range jobs {
				i, j := next.i, next.j
				target := heaps[i][j]
				errors <- computeSingleHeap(storage, metric, i, j, target)
			}
		}()
	}

	// start jobs
	go func() {
		for i, col := range dist {
			for j := range col {
				jobs <- job{i, j}
			}
		}
		close(jobs)
	}()

	// wait until done
	numDone := 0
	for _, col := range dist {
		for j := 0; j < len(col); j++ {
			nextErr := <-errors
			if nextErr != nil && err == nil {
				err = nextErr
			}
			numDone++
			if progress != nil {
				progress(numDone)
			}
		}
	}

	if err != nil {
		return nil, err
	}
	return heaps, nil
}

// HeapSelector is used to select the actual images after creating the image
// heaps.
type HeapSelector interface {
	Select(storage ImageStorage, query image.Image, dist TileDivision, heaps [][]*ImageHeap) ([][]ImageID, error)
}

// GenHeapViews can be used to transform the image heaps into the actual list
// of images in that heap.
// It's only a shortcut calling GetView on each heap.
func GenHeapViews(heaps [][]*ImageHeap) [][][]ImageHeapEntry {
	res := make([][][]ImageHeapEntry, len(heaps))
	for i, col := range heaps {
		size := len(col)
		colRes := make([][]ImageHeapEntry, size)
		for j := 0; j < size; j++ {
			heap := heaps[i][j]
			colRes[j] = heap.GetView()
		}
		res[i] = colRes
	}
	return res
}

// HeapImageSelector implements ImageSelector. It first computes the image
// heaps given the metric and then uses the provided HeapSelector to select
// the actual images from the heaps.
type HeapImageSelector struct {
	Metric      ImageMetric
	Selector    HeapSelector
	K           int
	NumRoutines int
}

// NewHeapImageSelector returns a new selector.
// Metric is the image metric that is used for the image heaps, selector is
// used to select the actual images from the heaps. k is the number of images
// stored in each image heap (that is the k best images are stored in the
// heaps).
// NumRoutines is the number of things that happen concurrently (not exactly,
// but guidance level),
func NewHeapImageSelector(metric ImageMetric, selector HeapSelector, k, numRoutines int) *HeapImageSelector {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	return &HeapImageSelector{
		Metric:      metric,
		Selector:    selector,
		K:           k,
		NumRoutines: numRoutines,
	}
}

// Init just calls InitStorage on the provided image metric.
func (sel *HeapImageSelector) Init(storage ImageStorage) error {
	return sel.Metric.InitStorage(storage)
}

// SelectImages first calls InitTiles on the provided metric, then computes
// the heaps and applies the selector on the heaps.
func (sel *HeapImageSelector) SelectImages(storage ImageStorage,
	query image.Image, dist TileDivision, progress ProgressFunc) ([][]ImageID, error) {
	if initErr := sel.Metric.InitTiles(storage, query, dist); initErr != nil {
		return nil, initErr
	}

	// compute heaps
	heaps, heapsErr := ComputeHeaps(storage, sel.Metric, query, dist, sel.K,
		sel.NumRoutines, progress)
	if heapsErr != nil {
		return nil, heapsErr
	}

	// apply selector
	return sel.Selector.Select(storage, query, dist, heaps)
}

// RandomHeapSelector implements HeapSelector by using just a random element
// from each heap.
//
// Note that instances of this selector are not safe for concurrent use.
type RandomHeapSelector struct {
	randGen *rand.Rand
}

// NewRandomHeapSelector returns a new random selector.
// The provided random generator is used to generate random numbers. You can
// use nil and a random generator will be created.
//
// Note that rand.Rand instances are not safe for concurrent use.
// Thus using the same generator on two instances that run concurrently is
// not allowed.
func NewRandomHeapSelector(randGen *rand.Rand) *RandomHeapSelector {
	if randGen == nil {
		seed := time.Now().UnixNano()
		randGen = rand.New(rand.NewSource(seed))
	}
	return &RandomHeapSelector{randGen}
}

// Select implements the HeapSelector interface, it selects the random images.
func (sel *RandomHeapSelector) Select(storage ImageStorage, query image.Image, dist TileDivision, heaps [][]*ImageHeap) ([][]ImageID, error) {
	res := make([][]ImageID, len(dist))

	views := GenHeapViews(heaps)

	for i, col := range dist {
		size := len(col)
		colDist := make([]ImageID, size)

		for j := 0; j < size; j++ {
			view := views[i][j]
			// select a random one
			n := len(view)
			if n == 0 {
				res[i][j] = NoImageID
			} else {
				// there are elements
				index := sel.randGen.Intn(n)
				colDist[j] = view[index].Image
			}
		}
		res[i] = colDist
	}
	return res, nil
}

// RandomHeapImageSelector returns a HeapImageSelector using a random selection.
// Thus it can be used as an ImageSelector.
func RandomHeapImageSelector(metric ImageMetric, k, numRoutines int) *HeapImageSelector {
	heapSel := NewRandomHeapSelector(nil)
	return NewHeapImageSelector(metric, heapSel, k, numRoutines)
}
