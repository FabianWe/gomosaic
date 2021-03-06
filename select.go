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
//
// This is because we might want to use an selector multiple times, though
// no such approach is implemented at the moment.
type ImageSelector interface {
	// Init is called each time the storage changes and at creation.
	// Note that in general a selector is not responsible for syncing histograms
	// with filesystem files etc. This should happen outside the storage.
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
	SelectImages(storage ImageStorage, query image.Image, dist TileDivision,
		progress ProgressFunc) ([][]ImageID, error)
}

// ImageMetric is used to compare a database image (image identified by an id)
// and a tile (previously registered) and return a metric value between the
// database image and the tile.
//
// It is used in ImageMetricMinimizer which contains more information.
//
// You might want to use InitTilesHelper to easily crate a concurrent
// InitTiles function.
//
// An example implementation is given in HistogramImageMetric.
type ImageMetric interface {
	InitStorage(storage ImageStorage) error
	InitTiles(storage ImageStorage, query image.Image, dist TileDivision) error
	Compare(storage ImageStorage, image ImageID, tileY, tileX int) (float64, error)
}

// InitTilesHelper is a helper function to easily create a concurrent InitTiles
// function for ImageMetrics.
//
// It will do the following: First it creates the actual tiles of the image
// and then it will call the init function. In this function you don't have
// to create metric values or something, just initialize the datastructure
// that holds information.
// It then gets concurrently filled by calls of onTile which is concurrently
// called for each tile of the image.
func InitTilesHelper(storage ImageStorage, query image.Image, dist TileDivision,
	numRoutines int,
	init func(tiles Tiles) error,
	onTile func(i, j int, tileImage image.Image) error) error {
	tiles, tilesErr := DivideImage(query, dist, numRoutines)
	if tilesErr != nil {
		return tilesErr
	}

	// initialize tile data
	if initErr := init(tiles); initErr != nil {
		return initErr
	}

	type job struct {
		i, j int
	}

	// compute data for each tile
	jobs := make(chan job, BufferSize)
	errors := make(chan error, BufferSize)

	// set later
	var err error

	for w := 0; w < numRoutines; w++ {
		go func() {
			for next := range jobs {
				tileImage := tiles[next.i][next.j]
				errors <- onTile(next.i, next.j, tileImage)
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
	for _, col := range dist {
		for j := 0; j < len(col); j++ {
			nextErr := <-errors
			if nextErr != nil && err == nil {
				err = nextErr
			}
		}
	}
	return err
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
// is called. Again, it is not the job of the metric to keep in sync with a
// filesystem / web resource whatever.
// Then for a query once InitTiles is called on the metric. In this step
// information about the query image are computed, for example computing GCHs
// of the query tiles. Then multiple calls to compare are made.
// To get the actual tiles from an image and a distribution use DivideImage.
//
// A note for more sophisticated storage approaches: At the moment all metric
// storages (for example histogram storage) have all the data fastly available
// in memory. This makes it easy to access an histogram.
// Here we iterate for each tile and then over each database image. This might
// be bad if the histograms for the database images are not loaded in memory
// and need to be opened from a file or database. Caches won't work fine because
// we iterate all database images, process to the next tile and repeat
// that. Thus an alternative version should be implemented iterating over the
// database images and then over the tiles, making it easier to cache things.
// However this requires more communication that is not necessary at the
// moment and so this implementation works fine as long as all information is
// in memory.
//
// The minimizer ignores metric errors in the way that whenever Compare
// returns an error != nil the candidate will be omitted. However a message will
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

// SelectImages selects the image that minimizes the metric for each tile.
// It computes the most fitting image for NumRoutines tiles concurrently.
func (min *ImageMetricMinimizer) SelectImages(storage ImageStorage,
	query image.Image, dist TileDivision, progress ProgressFunc) ([][]ImageID, error) {
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

	// compute best matching images

	numImages := storage.NumImages()
	var wg sync.WaitGroup
	wg.Add(numTiles)
	// job type
	type job struct {
		i, j int
	}
	jobs := make(chan job, BufferSize)

	// workers
	for w := 0; w < min.NumRoutines; w++ {
		go func() {
			for next := range jobs {
				var imageID ImageID
				for ; imageID < numImages; imageID++ {
					// try to compute distance and update entry
					dist, distErr := min.Metric.Compare(storage, imageID, next.i, next.j)
					if distErr != nil {
						log.WithFields(log.Fields{
							log.ErrorKey: distErr,
							"image":      imageID,
							"tileY":      next.i,
							"tileX":      next.j,
						}).Error("Can't compute metric value, ignoreing it")
						continue
					}
					// check if better than best so far
					if dist < bestValues[next.i][next.j] {
						bestValues[next.i][next.j] = dist
						result[next.i][next.j] = imageID
					}
				}
				wg.Done()
			}
		}()
	}

	// add jobs
	numDone := 0
	go func() {
		for i, inner := range dist {
			size := len(inner)
			for j := 0; j < size; j++ {
				jobs <- job{i, j}
				numDone++
				if progress != nil {
					progress(numDone)
				}
			}
		}
		close(jobs)
	}()

	wg.Wait()
	return result, nil
}

// HistogramImageMetric implements ImageMetric by keeping a histogram storage
// and computing histograms for a query image.
type HistogramImageMetric struct {
	HistStorage HistogramStorage
	Metric      HistogramMetric
	TileData    [][]*Histogram
	K           uint
	NumRoutines int
}

// NewHistogramImageMetric returns a new histogram image metric given a metric
// function between to histograms and the histogram storage to back the image
// metric. NumRoutines is the number of things that run concurrently when
// initializing the tile histograms.
func NewHistogramImageMetric(storage HistogramStorage, metric HistogramMetric, numRoutines int) *HistogramImageMetric {
	return &HistogramImageMetric{HistStorage: storage,
		Metric:      metric,
		TileData:    nil,
		K:           storage.Divisions(),
		NumRoutines: numRoutines}
}

// InitStorage does at the moment nothing.
func (m *HistogramImageMetric) InitStorage(storage ImageStorage) error {
	return nil
}

// InitTiles concurrently computes the histograms of the tiles of the query
// image.
func (m *HistogramImageMetric) InitTiles(storage ImageStorage, query image.Image, dist TileDivision) error {
	init := func(tiles Tiles) error {
		m.TileData = make([][]*Histogram, len(tiles))
		for i, col := range tiles {
			size := len(col)
			m.TileData[i] = make([]*Histogram, size)
		}
		return nil
	}
	onTile := func(i, j int, tileImage image.Image) error {
		m.TileData[i][j] = GenHistogram(tileImage, m.K, true)
		return nil
	}
	return InitTilesHelper(storage, query, dist, m.NumRoutines, init, onTile)
}

// Compare compares a database image and a query image based on the histogram
// metric function.
func (m *HistogramImageMetric) Compare(storage ImageStorage, image ImageID, tileY, tileX int) (float64, error) {
	// get histogram data for database image
	hDatabase, dbErr := m.HistStorage.GetHistogram(image)
	if dbErr != nil {
		return -1.0, dbErr
	}
	// get histogram for tile
	hTile := m.TileData[tileY][tileX]
	return m.Metric(hTile, hDatabase), nil
}

// GCHSelector is an image selector that selects images that minimize the
// histogram metric function Δ. Formally it is an ImageMetricMinimizer
// and thus implements ImageSelector.
func GCHSelector(histStorage HistogramStorage, delta HistogramMetric, numRoutines int) *ImageMetricMinimizer {
	imageMetric := NewHistogramImageMetric(histStorage, delta, numRoutines)
	return NewImageMetricMinimizer(imageMetric, numRoutines)
}

// LCHImageMetric implements ImageMetric by building the LCH sum, that is
// |Δ(h1[1], h2[1])| + ... + |Δ(h1[n], h2[n])| where Δ is a histogram metric.
type LCHImageMetric struct {
	LCHStorage LCHStorage
	Scheme     LCHScheme
	Metric     HistogramMetric
	TileData   [][]*LCH
	// we don't really have to save it, but it won't hurt
	// better than calling storage.Divisions again and again
	K           uint
	NumRoutines int
}

// NewLCHImageMetric returns a new LCH based metric.
func NewLCHImageMetric(storage LCHStorage, scheme LCHScheme, metric HistogramMetric, numRoutines int) *LCHImageMetric {
	return &LCHImageMetric{
		LCHStorage:  storage,
		Scheme:      scheme,
		Metric:      metric,
		TileData:    nil,
		K:           storage.Divisions(),
		NumRoutines: numRoutines,
	}
}

// InitStorage does nothing.
func (m LCHImageMetric) InitStorage(storage ImageStorage) error {
	return nil
}

// InitTiles concurrently computes the LCHs of the tiles of the query
// image.
func (m *LCHImageMetric) InitTiles(storage ImageStorage, query image.Image, dist TileDivision) error {
	init := func(tiles Tiles) error {
		m.TileData = make([][]*LCH, len(tiles))
		for i, col := range tiles {
			size := len(col)
			m.TileData[i] = make([]*LCH, size)
		}
		return nil
	}
	onTile := func(i, j int, tileImage image.Image) error {
		lch, lchErr := GenLCH(m.Scheme, tileImage, m.K, true)
		if lchErr != nil {
			return lchErr
		}
		m.TileData[i][j] = lch
		return nil
	}
	return InitTilesHelper(storage, query, dist, m.NumRoutines, init, onTile)
}

// Compare compares a database image and a query image based on the histogram
// metric function.
func (m *LCHImageMetric) Compare(storage ImageStorage, image ImageID, tileY, tileX int) (float64, error) {
	// get histogram data for database image
	lchDatabase, dbErr := m.LCHStorage.GetLCH(image)
	if dbErr != nil {
		return -1.0, dbErr
	}
	// get histogram for tile
	lchTile := m.TileData[tileY][tileX]
	return lchDatabase.Dist(lchTile, m.Metric)
}

// LCHSelector is an image selector that selects images that minimize the
// LCH distance |Δ(h1[1], h2[1])| + ... + |Δ(h1[n], h2[n])| where Δ is a
// histogram metric. Formally it is an ImageMetricMinimizer and thus implements
// ImageSelector.
func LCHSelector(storage LCHStorage, scheme LCHScheme, metric HistogramMetric, numRoutines int) *ImageMetricMinimizer {
	imageMetric := NewLCHImageMetric(storage, scheme, metric, numRoutines)
	return NewImageMetricMinimizer(imageMetric, numRoutines)
}
