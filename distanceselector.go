// Copyright 2019 Fabian Wenzelmann
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

// intManhattanDist returns the manhattan distance of two two-dimensional points
// (x1, y1) and (x2, y2).
func intManhattanDist(p1, p2 image.Point) int {
	return IntAbs(p1.X-p2.X) + IntAbs(p1.Y-p2.Y)
}

func getClosestManhattan(p image.Point, comparePoints []image.Point) int {
	currentMin := MaxInt
	for _, cmp := range comparePoints {
		dist := intManhattanDist(p, cmp)
		if dist < currentMin {
			currentMin = dist
		}
	}
	return currentMin
}

type assignedImageMap map[ImageID][]image.Point

func newAssignedImageMap(storage ImageStorage) assignedImageMap {
	result := make(assignedImageMap, int(storage.NumImages()))
	return result
}

func (m assignedImageMap) assignImage(img ImageID, tile image.Point) {
	if _, hasImage := m[img]; !hasImage {
		m[img] = make([]image.Point, 0, 1)
	}
	m[img] = append(m[img], tile)
}

func (m assignedImageMap) getAssigned(img ImageID) []image.Point {
	if res, has := m[img]; has {
		return res
	}
	return nil
}

type DistanceHeapSelector struct {
	Metric      ImageMetric
	K           int
	NumRoutines int
}

func NewDistanceHeapSelector(metric ImageMetric, k, numRoutines int) *DistanceHeapSelector {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	return &DistanceHeapSelector{
		Metric:      metric,
		K:           k,
		NumRoutines: numRoutines,
	}
}

func (selector *DistanceHeapSelector) Init(storage ImageStorage) error {
	return selector.Metric.InitStorage(storage)
}

func (selector *DistanceHeapSelector) SelectImages(storage ImageStorage,
	query image.Image, dist TileDivision, progress ProgressFunc) ([][]ImageID, error) {
	if initErr := selector.Metric.InitTiles(storage, query, dist); initErr != nil {
		return nil, initErr
	}
	// computes heaps
	heaps, heapsErr := ComputeHeaps(storage, selector.Metric, query, dist, selector.K,
		selector.NumRoutines, progress)
	if heapsErr != nil {
		return nil, heapsErr
	}
	// first create a new mapping from image --> tiles the image was used in
	currentAssignment := newAssignedImageMap(storage)
	result := make([][]ImageID, len(dist))

	// initialize result slices
	for i, inner := range dist {
		size := len(inner)
		result[i] = make([]ImageID, size)
		for j := 0; j < size; j++ {
			result[i][j] = NoImageID
		}
	}

	for i, inner := range dist {
		size := len(inner)
		for j := 0; j < size; j++ {
			// get rectangle for this tile
			rect := inner[j]
			currentPoint := rect.Min
			// now iterate over all images in the heap for this position
			// select the image with the smallest position
			// the assumption is that all images in the heap are considered a good candidate

			heap := heaps[i][j]
			view := heap.GetView()
			maxDist := MinInt
			bestImage := NoImageID
			for _, entry := range view {
				img := entry.Image
				dist := getClosestManhattan(currentPoint, currentAssignment.getAssigned(img))
				if dist > maxDist {
					maxDist = dist
					bestImage = img
				}
			}
			// assign image
			result[i][j] = bestImage
			currentAssignment.assignImage(bestImage, currentPoint)
		}
	}
	return result, nil
}
