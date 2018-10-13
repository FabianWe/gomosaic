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

type ImageHeapEntry struct {
	Image ImageID
	Value float64
}

func NewImageHeapEntry(image ImageID, value float64) ImageHeapEntry {
	return ImageHeapEntry{image, value}
}

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

type ImageHeap struct {
	interf *imageHeapInterface
	bound  int
}

func NewImageHeap(bound int) *ImageHeap {
	interf := newImageHeapInterface(bound)
	return &ImageHeap{interf, bound}
}

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

func (h *ImageHeap) Add(image ImageID, metricValue float64) {
	h.AddEntry(NewImageHeapEntry(image, metricValue))
}

// func (h *ImageHeap) Pop() ImageHeapEntry {
// 	x := heap.Pop(h.interf)
// 	return x.(ImageHeapEntry)
// }

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
