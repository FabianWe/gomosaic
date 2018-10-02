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

import "fmt"

// ImageMetric is any function that can compare two images (given by their id).
// Usually they should not load images into memory, but if it is required they
// cann access the image via the storage.
// If there is anything wrong the function should return an error.
// The smaller the metric value is the more equal the images are considered.
// Metric values should be ≥ 0.
type ImageMetric func(a, b ImageID, storage ImageStorage) (float64, error)

// HistogramMetric is a function that compares images based on histograms.
// It has no other input than the histograms, especially it has no access to
// the image. More complicated ImageMetrics based on histogram should be defined
// by another type.
// A HistogramMetric can assume that both histograms are defined for the same k.
// The smaller the metric value is the more equal the images are considered.
//
// Usually histogrm metrics operate on normalized histograms. The metric value
// should be ≥ 0.
type HistogramMetric func(hA, hB *Histogram) float64

// HistogramStorage maps image ids to histograms.
// By default the histograms should be normalized.
//
// Implementations must be safe for concurrent use.
type HistogramStorage interface {
	// GetHistogram returns the histogram for a previously registered ImageID.
	GetHistogram(id ImageID) (*Histogram, error)
}

// HistogramImageMetric creates a new image metric given a histogram metric
// and a histogram storage.
// The image mtric looks up both image ids in the histogram storage and
// returns the the histogram metric of those histograms. If one of the
// histograms cannot be received an error is returned.
func HistogramImageMetric(m HistogramMetric, storage HistogramStorage) ImageMetric {
	return func(a, b ImageID, iStorage ImageStorage) (float64, error) {
		hA, aErr := storage.GetHistogram(a)
		if aErr != nil {
			return -1.0, aErr
		}
		hB, bErr := storage.GetHistogram(b)
		if bErr != nil {
			return -1.0, bErr
		}
		if hA.K != hB.K {
			return -1.0, fmt.Errorf("Invalid histogram dimensions: %d != %d", hA.K, hB.K)
		}
		return m(hA, hB), nil
	}
}
