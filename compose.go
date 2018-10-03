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
	"errors"
	"image"
)

var (
	// ErrNoImage is the error returned by ImageSelectors if no image is
	// available.
	ErrNoImage = errors.New("No image selected")
)

// TODO change to select images?

// ImageSelector is used to select images for all tiles.
// The workflow is as follows: The selector gets initialized by calling Init,
// then several calls to SelectImage are made in order to retrieve images for
// the tiles.
type ImageSelector interface {
	// Init initializes the selector given the query image and the distribution
	// (= tiles).
	// In this step usually some procomputation is done, for example computing
	// GCHs or LCHs for each sub image.
	// To generate the tiles you can use DivideImage.
	Init(storage ImageStorage, query image.Image, dist TileDivision) error

	// SelectImage is called after Init and must return the most fitting image
	// for the tile described by the coordinates tileY and tileX. That is
	// dist[tileY][tileX] describes the tile for which an image should be
	// selected. query, storage and dist are the same as you got in the last call
	// to Init and are used here so you don't have to store the image and dist by
	// yourself in implementations. tileY and tileX are always legal values, that
	// is dist[tileY][tileX] is always safe to use.
	//
	// This step usually involves iterating over the precomputed data (for example
	// histograms for a database of images) and selecting the most fitting one.
	//
	// ImageMetricMinimizer is an example implementation.
	//
	// If no image can be selected (for example empty database) the function
	// should return ErrNoImage.
	SelectImage(storage ImageStorage, query image.Image, dist TileDivision, tileY, tileX int) (ImageID, error)
}

// ImageMetric is used to compare a database image (image identified by an id)
// and a tile (previously registered) and return a metric value between the
// database image and the tile.
//
// It is used in ImageMetricMinimizer which contains more information.
type ImageMetric interface {
	Init(storage ImageStorage, query image.Image, dist TileDivision) error
	Compare(storage ImageStorage, image ImageID, tileY, tileX int) (float64, error)
}

// ImageMetricMinimizer implements ImageSelector and selects the image with
// the smallest distance to the tile.
//
// It relies on a ImageMetric. The Init method simply calls the Init method
// of the provided metric.
//
// An example implementation is given in HistogramImageMetric.
type ImageMetricMinimizer struct {
	Metric ImageMetric
}

func NewImageMetricMinimizer(metric ImageMetric) *ImageMetricMinimizer {
	return &ImageMetricMinimizer{Metric: metric}
}

func (min *ImageMetricMinimizer) Init(storage ImageStorage, query image.Image, dist TileDivision) error {
	return min.Metric.Init(storage, query, dist)
}
