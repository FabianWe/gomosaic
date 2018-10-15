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
	"container/heap"
)

// ImageHeapEntry is an entry stored in an image heap. It consists of an image
// (by id) and the value of that image.
type ImageHeapEntry struct {
	Image ImageID
	Value float64
}

// NewImageHeapEntry returns a new heap entry.
func NewImageHeapEntry(image ImageID, value float64) ImageHeapEntry {
	return ImageHeapEntry{image, value}
}

// imageHeapInterface is an internal type that implements heap.Interface.
// We actually hide the implementation details and just allow Add operations.
type imageHeapInterface []ImageHeapEntry

func newImageHeapInterface(bound int) *imageHeapInterface {
	capacity := bound
	if capacity < 1 {
		capacity = 100
	}
	res := make(imageHeapInterface, 0, capacity)
	return &res
}

func (h imageHeapInterface) Len() int {
	return len(h)
}

func (h imageHeapInterface) Less(i, j int) bool {
	return h[i].Value > h[j].Value
}

func (h imageHeapInterface) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *imageHeapInterface) Push(x interface{}) {
	*h = append(*h, x.(ImageHeapEntry))
}

func (h *imageHeapInterface) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// ImageHeap is a container that stores images sorted according to a float
// value.
type ImageHeap struct {
	interf *imageHeapInterface
	bound  int
}

// NewImageHeap returns a new image heap with a given bound. If bound ≥ 0 it
// is used as the upper limit of entries stored in the heap. That is only
// the bound smallest images are stored.
func NewImageHeap(bound int) *ImageHeap {
	interf := newImageHeapInterface(bound)
	return &ImageHeap{interf, bound}
}

// AddEntry adds a new entry to the heap, truncating the heap if bounds is ≥ 0.
func (h *ImageHeap) AddEntry(entry ImageHeapEntry) {
	heap.Push(h.interf, entry)
	switch {
	case h.bound == 0:
		h.interf = nil
	case h.bound >= 1:
		for h.interf.Len() > h.bound {
			heap.Pop(h.interf)
		}
	}
}

// Add is a shortcut for AddEntry.
func (h *ImageHeap) Add(image ImageID, metricValue float64) {
	h.AddEntry(NewImageHeapEntry(image, metricValue))
}

// GetView returns the sorted collection of entries in the heap, that is images
// with smallest values first. The length of the result slice is between 0
// and bounds.
// The complexity is O(n * log(n)) where n is the size of the heap.
func (h *ImageHeap) GetView() []ImageHeapEntry {
	n := h.interf.Len()
	tmp := make(imageHeapInterface, n)
	copy(tmp, *h.interf)
	res := make([]ImageHeapEntry, n)
	for i := 0; i < n; i++ {
		x := heap.Pop(&tmp).(ImageHeapEntry)
		res[n-i-1] = x
	}
	return res
}
