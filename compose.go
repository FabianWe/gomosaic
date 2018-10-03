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
	"math"
	"sync"

	log "github.com/sirupsen/logrus"
)

// ImageSelector is used to select images for all tiles.
// The workflow is as follows: The selector gets initialized by calling Init,
// then images are selected with SelectImages.
type ImageSelector interface {
	// Init is called each time the storage changes, thus you can load precomputed
	// data, for example histograms, from a file.
	Init(storage ImageStorage) error

	// SelectImage is called after Init and returns the most fitting images for
	// the query. The returned images matrix must be of the same size as the
	// dist matrix.
	//
	// This step usually involves iterating over the precomputed data (for example
	// histograms for a database of images) and selecting the most fitting one.
	//
	// ImageMetricMinimizer is an example implementation.
	//
	// If no image can be selected (for example empty database) the id in the
	// result should be set to NoImageID.
	SelectImages(storage ImageStorage, query image.Image, dist TileDivision) ([][]ImageID, error)
}

// ImageMetric is used to compare a database image (image identified by an id)
// and a tile (previously registered) and return a metric value between the
// database image and the tile.
//
// It is used in ImageMetricMinimizer which contains more information.
//
// An example implementation is given in HistogramImageMetric.
type ImageMetric interface {
	InitStorage(storage ImageStorage) error
	InitTiles(storage ImageStorage, query image.Image, dist TileDivision) error
	Compare(storage ImageStorage, image ImageID, tileY, tileX int) (float64, error)
}

// ImageMetricMinimizer implements ImageSelector and selects the image with
// the smallest distance to the tile.
//
// It relies on a ImageMetric. The Init method simply calls the InitStorage
// method of the provided metric.
//
// Each time images should be selected it calls InitTiles on the metric and
// selects the best images.
//
// Thus the workflow is as follows: First the InitStorage method of the metric
// is called, that is the step in which precomputed information should be read
// from a file etc.
// Then for a query once InitTiles is called on the metric. In this step
// information about the query image are computed, for example computing GCHs
// of the query tiles. Then multiple calls to compare are made.
//
// A note for more sophisticated storage approaches: At the moment all metric
// storages (for example histgram storage) have all the data fastly available
// in memory. This makes it easy to access an histogram.
// Here we iterate for each tile and then over each database image. This might
// be bad if the histograms for the database images are not loaded in memory
// and need to be opened from a file or database. Caches won't work fine because
// we bascially iterate all database images, process to the next tile and repeat
// that. Thus an alternative version should be implemented iterating over the
// database images and then over the tiles, making it easier to cache things.
// However this requires more communication that is not necessary at the
// moment and so this implementation works fine as long as all information is
// in memory.
//
// The minimizer ignores metric errors in the way that whenever Compare
// returns an error != nil the candiate will be omitted. However a message will
// be logged in this case.
type ImageMetricMinimizer struct {
	Metric      ImageMetric
	NumRoutines int
}

// NewImageMetricMinimizer returns a new metric minimizer given the metric to
// use and the number of go routines to run when selecting images.
func NewImageMetricMinimizer(metric ImageMetric, numRoutines int) *ImageMetricMinimizer {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	return &ImageMetricMinimizer{Metric: metric, NumRoutines: numRoutines}
}

// Init just calls InitStorage of the metric.
func (min *ImageMetricMinimizer) Init(storage ImageStorage) error {
	return min.Metric.InitStorage(storage)
}

// TODO test with smaller buffer if everything is okay

// SelectImages selects the image that minimizes the metric for each tile.
// It computes the most fitting image for NumRoutines tiles concurrently.
func (min *ImageMetricMinimizer) SelectImages(storage ImageStorage, query image.Image, dist TileDivision) ([][]ImageID, error) {
	if initErr := min.Metric.InitTiles(storage, query, dist); initErr != nil {
		return nil, initErr
	}
	result := make([][]ImageID, len(dist))
	bestValues := make([][]float64, len(dist))

	// sum of all tiles, used later
	numTiles := 0

	// initialize slices
	for i, inner := range dist {
		size := len(inner)
		numTiles += size
		result[i] = make([]ImageID, size)
		bestValues[i] = make([]float64, size)
		for j := 0; j < size; j++ {
			result[i][j] = NoImageID
			bestValues[i][j] = math.MaxFloat64
		}
	}

	// compute best matching images, as explained in the documentation we iterate
	// over image ids first to improve memory usage
	// we use k workers concurrently

	numImages := storage.NumImages()
	var wg sync.WaitGroup
	wg.Add(numTiles)
	// job type
	type job struct {
		tileY, tileX int
	}
	jobs := make(chan job, 1000)

	// workers
	for w := 0; w < min.NumRoutines; w++ {
		go func() {
			for next := range jobs {
				var imageID ImageID
				for ; imageID < numImages; imageID++ {
					// try to compute distance and update entry
					dist, distErr := min.Metric.Compare(storage, imageID, next.tileY, next.tileX)
					if distErr != nil {
						log.WithFields(log.Fields{
							log.ErrorKey: distErr,
							"image":      imageID,
							"tileY":      next.tileY,
							"tileX":      next.tileX,
						}).Error("Can't compute metric value, ignoreing it")
						continue
					}
					// check if better than best so far
					if dist < bestValues[next.tileY][next.tileX] {
						bestValues[next.tileY][next.tileX] = dist
						result[next.tileY][next.tileX] = imageID
					}
				}
				wg.Done()
			}
		}()
	}

	// add jobs
	go func() {
		for i, inner := range dist {
			size := len(inner)
			for j := 0; j < size; j++ {
				jobs <- job{i, j}
			}
		}
		close(jobs)
	}()

	wg.Wait()
	return result, nil
}
