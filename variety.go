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

// TODO doc that it does not call InitTiles
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

type HeapSelector interface {
	Select(storage ImageStorage, query image.Image, dist TileDivision, heaps [][]*ImageHeap) ([][]ImageID, error)
}

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

type HeapImageSelector struct {
	Metric      ImageMetric
	Selector    HeapSelector
	K           int
	NumRoutines int
}

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

func (sel *HeapImageSelector) Init(storage ImageStorage) error {
	return sel.Metric.InitStorage(storage)
}

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

type RandomHeapSelector struct {
	randGen *rand.Rand
}

// TODO not safe for concurrent use

func NewRandomHeapSelector(randGen *rand.Rand) *RandomHeapSelector {
	if randGen == nil {
		seed := time.Now().UnixNano()
		randGen = rand.New(rand.NewSource(seed))
	}
	return &RandomHeapSelector{randGen}
}

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

func RandomHeapImageSelector(metric ImageMetric, k, numRoutines int) *HeapImageSelector {
	heapSel := NewRandomHeapSelector(nil)
	return NewHeapImageSelector(metric, heapSel, k, numRoutines)
}
