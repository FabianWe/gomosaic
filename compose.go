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
	"fmt"
	"image"
	"sync"

	log "github.com/sirupsen/logrus"
)

var (
	// ImageCacheSize is the default size of images caches. Some procedures
	// (especially the composition of mosaics) might be much more performant if
	// they're allowed to cache images. This variable controls the size of such
	// caches. It must be a value ≥ 1.
	ImageCacheSize = 15
)

// ResizeStrategy is a function that scales an image (img) to an image of
// exyctly the size defined by tileWidth and tileHeight.
// This is used to compose the mosaic when the selected database images must be
// resized to fit in the tiles.
//
// The difference between ResizeStrategy and ImageResizer is that we think of
// an ImageResizer as an "engine", for example a libarary, that performs the
// of scaling an image exactly to a specific width and height.
// A ResizeStrategy might first resize an image to some other size and then
// return a subimage. That is we think of a resizer as something that does the
// work and a ResizeStrategy as something that decides how to nicely scale an
// image s.t. it fits nicely.
type ResizeStrategy func(resizer ImageResizer, tileWidth, tileHeight uint, img image.Image) image.Image

// ForceResize is a resize strategy that resizes to the given width and height,
// ignoring the ration of the original image.
func ForceResize(resizer ImageResizer, tileWidth, tileHeight uint, img image.Image) image.Image {
	return resizer.Resize(tileWidth, tileHeight, img)
}

// TODO implement smarter strategies?

// TODO some smarter cache strategies?

// ImageCache is used to cache resized versions of images during mosaic
// generation. The same image with the same size might appear often in a mosaic
// (or the same area). This and the fact that resizing an image is not very fast
// makes it useful to cache the images.
//
// Caches are safe for concurrent use.
type ImageCache struct {
	m           *sync.Mutex
	size        int
	content     map[string]image.Image
	insertOrder []string
}

// NewImageCache returns an empty image cache. size is the number of images that
// will be cached. size must be ≥ 1.
func NewImageCache(size int) *ImageCache {
	if size <= 0 {
		size = 1
	}
	var m sync.Mutex
	return &ImageCache{
		m:           &m,
		size:        size,
		content:     make(map[string]image.Image, size),
		insertOrder: make([]string, 0, size),
	}
}

func (cache *ImageCache) keyFormat(id ImageID, width, height int) string {
	return fmt.Sprintf("%d-%d-%d", id, width, height)
}

func (cache *ImageCache) lookup(key string) image.Image {
	if img, has := cache.content[key]; has {
		return img
	}
	return nil
}

// Put adds an image to the cache. Usually Put is called after Get: If the
// image was not found in the cache it is scaled and then added to the cache via
// Put.
func (cache *ImageCache) Put(id ImageID, width, height int, img image.Image) {
	cache.m.Lock()
	defer cache.m.Unlock()
	keyFmt := cache.keyFormat(id, width, height)
	// first check if image already in cache, if yes do nothing
	if lookup := cache.lookup(keyFmt); lookup != nil {
		return
	}
	// check if cache is full
	if len(cache.insertOrder) < cache.size {
		cache.insertOrder = append(cache.insertOrder, keyFmt)
		cache.content[keyFmt] = img
	} else {
		// cache full, remove first element form cache
		// since size must be >= 1 this should be fine
		fst := cache.insertOrder[0]
		// remove from slice
		cache.insertOrder = cache.insertOrder[1:]
		cache.insertOrder = append(cache.insertOrder, keyFmt)
		// remove from map and add to map
		delete(cache.content, fst)
		cache.content[keyFmt] = img
	}
}

// Get returns the image from the cache. If the return value is nil the image
// was not found in the cache and should be added to the cache by Put.
func (cache *ImageCache) Get(id ImageID, width, height int) image.Image {
	cache.m.Lock()
	defer cache.m.Unlock()
	// check if item is in cache
	keyFmt := cache.keyFormat(id, width, height)
	return cache.lookup(keyFmt)
}

func insertTile(into *image.RGBA, area image.Rectangle, storage ImageStorage,
	dbImage ImageID, resizer ImageResizer, s ResizeStrategy,
	cache *ImageCache) error {
	// so sorry for the signature
	// read image
	tileWidth := area.Dx()
	tileHeight := area.Dy()
	if area.Empty() {
		return nil
	}
	var img image.Image
	// first try to lookup the image in the cache
	img = cache.Get(dbImage, tileWidth, tileHeight)
	if img == nil {
		var imgErr error
		// use storage to read image and then resize it
		img, imgErr = storage.LoadImage(dbImage)
		if imgErr != nil {
			return imgErr
		}
		// now resize the image given the strategy
		img = s(resizer, uint(tileWidth), uint(tileHeight), img)
		// add to cache
		cache.Put(dbImage, tileWidth, tileHeight, img)
	}
	scaledBounds := img.Bounds()
	for y := 0; y < tileHeight; y++ {
		for x := 0; x < tileWidth; x++ {
			// get color from scaled image
			c := img.At(scaledBounds.Min.X+x, scaledBounds.Min.Y+y)
			// set color
			into.Set(area.Min.X+x, area.Min.Y+y, c)
		}
	}
	return nil
}

// ComposeMosaic concurrently composes a mosaic image given the distribution
// in tiles and the selected images for each tile.
// Images are loaded by the storage. The resizer and the resize strategy
// are used to resize database images to fit in tiles. The mosaic division must
// start from (0, 0) and the rectangles are not allowed to overlap, in short
// it has be what we intuively would call a distribution into tiles.
//
// Scaled database images are cached to speed up the generation process.
// The cache size parameter is the size of the cache used. The more elements in
// the cache the faster the composition process is, but it also increases
// memory consumption. If cache size is ≤ 0 the DefaultCacheSize is used.
func ComposeMosaic(storage ImageStorage, symbolicTiles [][]ImageID,
	mosaicDivison TileDivision, resizer ImageResizer, s ResizeStrategy,
	numRoutines, cacheSize int, progress ProgressFunc) (image.Image, error) {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	if cacheSize <= 0 {
		cacheSize = ImageCacheSize
	}

	numTilesVert := len(symbolicTiles)

	// first create an empty image
	res := image.NewRGBA(image.Rectangle{})
	if numTilesVert == 0 {
		return res, nil
	}
	lastCol := symbolicTiles[numTilesVert-1]
	if len(lastCol) == 0 {
		return res, nil
	}
	lastTile := mosaicDivison[numTilesVert-1][len(lastCol)-1]
	// this should be correct because the rectangles are arranged from (0, 0)
	// to (width, height)
	resBounds := image.Rect(0, 0, lastTile.Max.X, lastTile.Max.Y)
	if resBounds.Empty() {
		return nil, errors.New("Can't compose mosaic: Image would be empty")
	}
	res = image.NewRGBA(resBounds)
	cache := NewImageCache(cacheSize)

	type job struct {
		i, j int
	}
	jobs := make(chan job, BufferSize)
	done := make(chan bool, BufferSize)

	for w := 0; w < numRoutines; w++ {
		go func() {
			for next := range jobs {
				tilesCol, divisionCol := symbolicTiles[next.i], mosaicDivison[next.i]
				tileArea, dbImage := divisionCol[next.j], tilesCol[next.j]
				if dbImage == NoImageID {
					log.WithFields(log.Fields{
						"area": tileArea,
					}).Warn("No image found for tile")
				} else {
					insertTile(res, tileArea, storage, dbImage, resizer, s, cache)
				}
				done <- true
			}
		}()
	}

	// start jobs
	go func() {
		for i, tilesCol := range symbolicTiles {
			for j := 0; j < len(tilesCol); j++ {
				jobs <- job{i, j}
			}
		}
		close(jobs)
	}()

	// wait until done
	numDone := 0
	for _, tilesCol := range symbolicTiles {
		for j := 0; j < len(tilesCol); j++ {
			<-done
			numDone++
			if progress != nil {
				progress(numDone)
			}
		}
	}

	return res, nil
}
