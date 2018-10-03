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
// TODO we could use some concurrency here, butprobably resizing is also
// running concurrently... so would be nice but it's okay?

func insertTile(into *image.RGBA, area image.Rectangle, storage ImageStorage,
	dbImage ImageID, resizer ImageResizer, s ResizeStrategy) error {
	// read image
	img, imgErr := storage.LoadImage(dbImage)
	if imgErr != nil {
		return imgErr
	}
	// now resize the image given the strategy
	tileWidth := area.Dx()
	tileHeight := area.Dy()
	img = s(resizer, uint(tileWidth), uint(tileHeight), img)
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

// doc: mosaic division must start from(0, 0)
func ComposeMosaic(storage ImageStorage, symbolicTiles [][]ImageID,
	mosaicDivison TileDivision, resizer ImageResizer, s ResizeStrategy) (image.Image, error) {
	// TODO is this correct???
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
	res = image.NewRGBA(resBounds)
	for i := 0; i < numTilesVert; i++ {
		tilesCol := symbolicTiles[i]
		divisionCol := mosaicDivison[i]
		lenCol := len(tilesCol)
		for j := 0; j < lenCol; j++ {
			tileArea := divisionCol[j]
			dbImage := tilesCol[j]
			insertTile(res, tileArea, storage, dbImage, resizer, s)
		}
	}

	return res, nil
}
