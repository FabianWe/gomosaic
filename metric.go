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
	"math"
	"strings"
)

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

// VectorMetric is a function that takes two vectors of the same length and
// returns a metric value ("distance") of the two.
//
// Vector metrics therefor can be used for comparing histograms.
type VectorMetric func(p, q []float64) float64

// HistogramVectorMetric converts a vector metric to a histogram metric.
// With HistogramImageMetric a vector metric can be converted to an image
// metric.
func HistogramVectorMetric(vm VectorMetric) HistogramMetric {
	return func(hA, hB *Histogram) float64 {
		return vm(hA.Entries, hB.Entries)
	}
}

// Manhattan returns the manhattan distance of two vectors, that is
// |p1 - q1| + ... + |pn - qn|.
func Manhattan(p, q []float64) float64 {
	var result float64
	for i, e1 := range p {
		result += math.Abs(e1 - q[i])
	}
	return result
}

// EuclideanDistance returns the euclidean distance of two
// vectors, that is sqrt( (p1 - q1)² + ... + (pn - qn)² ).
func EuclideanDistance(p, q []float64) float64 {
	var sum float64
	for i, e1 := range p {
		e2 := q[i]
		diff := (e1 - e2)
		sum += (diff * diff)
	}
	return math.Sqrt(sum)
}

// MinDistance returns 1 - ( min(p1, q1) + ... + min(pn, qn) ).
func MinDistance(p, q []float64) float64 {
	var sum float64
	for i, e1 := range p {
		e2 := q[i]
		sum += math.Min(e1, e2)
	}
	return 1.0 - sum
}

// CosineSimilarity returns 1 - cos(∡(p, q)). The result is between 0 and 2,
// as special case is that the length of p or q is 0, in this case the result
// is 2.1
func CosineSimilarity(p, q []float64) float64 {
	var dotProduct, lengthP, lengthQ float64
	for i, e1 := range p {
		e2 := q[i]
		dotProduct += (e1 * e2)
		lengthP += (e1 * e1)
		lengthQ += (e2 * e2)
	}
	if lengthP == 0.0 || lengthQ == 0.0 {
		// special case if a vector should be "empty", in this case we return
		// a big distance for this metric (max value should be 2)
		return 2.1
	}
	lengthP = math.Sqrt(lengthP)
	lengthQ = math.Sqrt(lengthQ)
	return 1.0 - (dotProduct / (lengthP * lengthQ))
}

// ChessboardDistance is the max over all absolute distances,
// see https://reference.wolfram.com/language/ref/ChessboardDistance.html
func ChessboardDistance(p, q []float64) float64 {
	res := 0.0
	for i, e1 := range p {
		e2 := q[i]
		res = math.Max(res, math.Abs(e1-e2))
	}
	return res
}

// CanberraDistance is a weighted version of the manhattan
// distance, see https://en.wikipedia.org/wiki/Canberra_distance
func CanberraDistance(p, q []float64) float64 {
	res := 0.0
	for i, e1 := range p {
		e2 := q[i]
		numerator := math.Abs(e1 - e2)
		// assuming all values are positive the Abs is not required
		denominator := math.Abs(e1) + math.Abs(e2)
		res += (numerator / denominator)
	}
	return res
}

// The following variables are used for registering named
// metrics.

var (
	histogramMetrics map[string]HistogramMetric
)

// RegisterHistogramMetric is used to register a named histogram
// metric. It will only add the metric if the name does not
// exist yet. The result is true if the metric was successfully
// registered and false otherwise.
// Some metrics are registered by default.
// All names must be lowercase strings, the register and get
// methods will always transform a string to lowercase.
//
// All metrics should be registered by an init method.
func RegisterHistogramMetric(name string, metric HistogramMetric) bool {
	name = strings.ToLower(name)
	if _, has := histogramMetrics[name]; has {
		return false
	}
	histogramMetrics[name] = metric
	return true
}

// GetHistogramMetricNames returns a list of all registered
// named histogram metrics. See RegisterHistogramMetric for
// details.
func GetHistogramMetricNames() []string {
	res := make([]string, 0, len(histogramMetrics))
	for key := range histogramMetrics {
		res = append(res, key)
	}
	return res
}

// GetHistogramMetric returns a registered histogram metric.
// Returns the metric and true on success and nil and false
// otherwise.
// See RegisterHistogramMetric for details.
func GetHistogramMetric(name string) (HistogramMetric, bool) {
	name = strings.ToLower(name)
	if metric, has := histogramMetrics[name]; has {
		return metric, true
	}
	return nil, false
}

func init() {
	histogramMetrics = make(map[string]HistogramMetric)
	RegisterHistogramMetric("manhattan", HistogramVectorMetric(Manhattan))
	RegisterHistogramMetric("euclid", HistogramVectorMetric(EuclideanDistance))
	RegisterHistogramMetric("min", HistogramVectorMetric(MinDistance))
	RegisterHistogramMetric("cosine", HistogramVectorMetric(CosineSimilarity))
	RegisterHistogramMetric("chessboard", HistogramVectorMetric(ChessboardDistance))
	RegisterHistogramMetric("canberra", HistogramVectorMetric(CanberraDistance))
}
