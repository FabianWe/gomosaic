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
	"image"

	log "github.com/sirupsen/logrus"
)

// DivideMode is used to describe in which way to handle remaining pixels
// in image division.
// The exact meaning might differ (depending on the arranger) but as an example
// consider an image with 99 pixels width. If we want to divide the image into
// tiles with 10 pixels. This leads to a 9 tiles with 10 pixels, but 9 pixels
// are left. DivideMode now describes what to do with the remaining 9 pixels:
// Crop would mean to crop the image and discard the remaining pixels.
// Adjust would mean to adjust the last tile to have a width of 9 and pad
// would mean to add an additional tile with width 10 (and thus describing a
// tile that does not intersect with the image everywhere).
type DivideMode int

const (
	// DivideCrop is the mode in which remaining pixels are discarded.
	DivideCrop DivideMode = iota
	// DivideAdjust is the mode in which a tile is adjusted to the remaining
	// pixels.
	DivideAdjust
	// DividePad is the mode in which a tile of a certain size is created even
	// if not enough pixels are remaining.
	DividePad
)

func (mode DivideMode) String() string {
	switch mode {
	case DivideCrop:
		return "DivideCrop"
	case DivideAdjust:
		return "DivideAdjust"
	case DividePad:
		return "DividePad"
	default:
		return fmt.Sprintf("DivideMode(%d)", mode)
	}
}

// TileDivision represents the divison of an image into rectangles. See
// ImageDivider for details about divisions.
//
// Tiles are not stored in the fashion (x, y) but (y, x). That means each entry
// in the division describes one column of the image.
// The get method does this correctly.
type TileDivision [][]image.Rectangle

// Get returns the rectangle at position div[y][x], that is the rectangle
// in row y and column x.
func (div TileDivision) Get(x, y int) image.Rectangle {
	return div[y][x]
}

// Tiles are the tiles of an image. They're genrated from a TileDivision
// and the image matrix is of the same size as the TileDivision.
//
// Tiles are not stored in the fashion (x, y) but (y, x). That means each entry
// in the division describes one column of the image.
// The get method does this correctly.
type Tiles [][]image.Image

// Get returns the image at position tiles[y][x], that is the image in row y and
// column x.
func (tiles Tiles) Get(x, y int) image.Image {
	return tiles[y][x]
}

// ImageDivider is a type to divide an image into tiles. That is it creates
// the areas which should be replaced by images from the database.
//
// The returned distribution has to meet the following requirements:
//
// (1) It returns a matrix of rectangles. That is the results contains
// rows and each row has the same length. So the element at (0, 0) describes
// the first rectangle in the image (top left corner).
//
// (2) Rectangles might be of different size.
//
// (3) The rectangle is not required to be a part of the image. In fact it
// must not even overlap with the image at some point, but usually it should.
//
// (4) The result may be empty (or nil); rows may be empty.
//
// (5) Images are stored in coordinates [y][x], that means each entry in the
// tile division describes a column.
type ImageDivider interface {
	Divide(image.Rectangle) TileDivision
}

// FixedSizeDivider divides an image into tiles where each tile has the
// given width and height. It implements ImageDivider.
// The DivideMode describes how to deal with "remaining" pixel.
type FixedSizeDivider struct {
	Width, Height int
	Mode          DivideMode
}

// NewFixedSizeDivider returns a new FixedSizeDivider.
func NewFixedSizeDivider(width, height int, mode DivideMode) FixedSizeDivider {
	return FixedSizeDivider{Width: width, Height: height, Mode: mode}
}

func (divider FixedSizeDivider) getSize(originalDimension, tileDimension int) int {
	switch {
	case tileDimension > originalDimension, tileDimension == 0:
		return 1
	case originalDimension%tileDimension == 0:
		return originalDimension / tileDimension
	default:
		switch divider.Mode {
		case DivideCrop:
			return originalDimension / tileDimension
		default:
			return (originalDimension / tileDimension) + 1
		}
	}
}

func (divider FixedSizeDivider) outerBound(imgBoundPosition, position int) int {
	switch {
	case position <= imgBoundPosition:
		return position
	case divider.Mode == DivideAdjust:
		return imgBoundPosition
	default:
		// now mode must be DividePad, for crop we should never end up here
		if Debug {
			if divider.Mode != DividePad {
				log.Warn("Got divide mode", divider.Mode, "expected", DividePad)
			}
		}
		return position
	}
}

// TODO test this (test cases,not just real world)

// Divide implements the Divide method of ImageDivider.
func (divider FixedSizeDivider) Divide(bounds image.Rectangle) TileDivision {
	// no division possible if bounds are empty
	if bounds.Empty() {
		return nil
	}
	imgWidth := bounds.Dx()
	imgHeight := bounds.Dy()

	numRows := divider.getSize(imgHeight, divider.Height)
	numCols := divider.getSize(imgWidth, divider.Width)
	res := make(TileDivision, numRows)
	for i := 0; i < numRows; i++ {
		res[i] = make([]image.Rectangle, numCols)
		for j := 0; j < numCols; j++ {
			x0 := bounds.Min.X + j*divider.Width
			y0 := bounds.Min.Y + i*divider.Height
			x1 := divider.outerBound(bounds.Max.X, x0+divider.Width)
			y1 := divider.outerBound(bounds.Max.Y, y0+divider.Height)
			res[i][j] = image.Rect(x0, y0, x1, y1)
		}
	}
	return res
}

// FixedNumDivider is an ImageDivider that divides an image into a given number
// of tiles.
// Cut describes whether the image should be "cut".
// Cutting means to cut the resulting image s.t. each tile has the same bounds.
// Example: Suppose you want to divide an image with width 99 and want ten
// want to tiles horizontally. This leads to an image where each tile has
// a width of 9. Ten tiles yields to a final width of 90. As you see 9 pixels
// are "left over". The distribution in ten tiles is fixed, so we can't add
// another tile. But in order to enforce the original propsed width
// we can enlarge the last tile by 9 pixels. So we would have 9 tiles with
// width 9 and one tile with width 18.
//
// Cut controls what to do with those remaining pixels: If cut is set
// to true we skip the 9 pixels and return an image of size 90. If set to
// false we enlarge the last tile and return an image witz size 99.
type FixedNumDivider struct {
	NumX, NumY int
	Cut        bool
}

// NewFixedNumDivider returns a new FixedNumDivider given the number of tiles in
// x and y direction.
func NewFixedNumDivider(numX, numY int, cut bool) *FixedNumDivider {
	return &FixedNumDivider{NumX: numX, NumY: numY, Cut: cut}
}

// divisionNum either row or column
func (divider *FixedNumDivider) outerBound(divisionNum, index, imgBound, value int) int {
	if index+1 == divisionNum {
		// we're in the last row / column, depending on cut decide what to do
		if divider.Cut {
			// we cut the image, thus return the value
			return value
		}
		// don't cut image, thus the rectangle becomes larger, return the bound
		return imgBound
	}
	return value
}

// Divide implements the Divide method of ImageDivider.
func (divider *FixedNumDivider) Divide(bounds image.Rectangle) TileDivision {
	// similar to FixedSizeArranger, but forces the dimensions
	// no division possible if empty
	if bounds.Empty() {
		return nil
	}
	imgWidth := bounds.Dx()
	imgHeight := bounds.Dy()

	// some sane defaults if numX or numY should be 0, just to be sure
	tileWidth := 1
	tileHeight := 1
	if divider.NumX > 0 {
		tileWidth = imgWidth / divider.NumX
	}
	if divider.NumY > 0 {
		tileHeight = imgHeight / divider.NumY
	}
	// this should take care of images that are too small, if such small images
	// are used the results will be bad I guess, this is just a way to ensure
	// that some part of the image is used
	if tileWidth <= 0 {
		tileWidth = 1
	}
	if tileHeight <= 0 {
		tileHeight = 1
	}
	numRows := divider.NumY
	numCols := divider.NumX
	res := make(TileDivision, divider.NumY)
	for i := 0; i < numRows; i++ {
		res[i] = make([]image.Rectangle, numCols)
		for j := 0; j < numCols; j++ {
			x0 := bounds.Min.X + j*tileWidth
			y0 := bounds.Min.Y + i*tileHeight
			// TODO think this through again...
			x1 := divider.outerBound(numCols, j, bounds.Max.X, x0+tileWidth)
			y1 := divider.outerBound(numRows, i, bounds.Max.Y, y0+tileHeight)
			res[i][j] = image.Rect(x0, y0, x1, y1)
		}
	}
	return res
}

// DivideImage computes the actual tiles from an image and the distribution
// into tile rectangles.
// The returned images should all be part of the image, thus must not have the
// same size as suggested by the distribution.
func DivideImage(img image.Image, distribution TileDivision, numRoutines int) (Tiles, error) {
	if numRoutines <= 0 {
		numRoutines = 1
	}
	bounds := img.Bounds()
	res := make(Tiles, len(distribution))
	// any error that occurs sets this variable (first error)
	// this is done later
	var err error

	// struct that we use for the channel
	type job struct {
		i, j int
	}

	jobs := make(chan job, BufferSize)
	errorChan := make(chan error, BufferSize)

	for w := 0; w < numRoutines; w++ {
		go func() {
			for next := range jobs {
				r := distribution[next.i][next.j]
				// first intersect tom ake sure that we truly have a rectangle in the image
				r = r.Intersect(bounds)
				// now we try to get the subimage
				// because the intersection can be empty the computed image can be
				// empty as well
				subImg, subErr := SubImage(img, r)
				res[next.i][next.j] = subImg
				errorChan <- subErr
			}
		}()
	}
	go func() {
		for i, col := range distribution {
			// initialize res[i]
			res[i] = make([]image.Image, len(col))
			for j := 0; j < len(col); j++ {
				jobs <- job{i, j}
			}
		}
		close(jobs)
	}()
	for _, col := range distribution {
		for j := 0; j < len(col); j++ {
			nextErr := <-errorChan
			if nextErr != nil && err != nil {
				err = nextErr
			}
		}
	}
	return res, err
}
