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
)

// MosaicArranger is a type to divide an image into tiles. That is it creates
// the areas which should be replaced by images from the database.
//
// It can assume that the image isn't empty.
//
// The returned distribution has to meet the following requirements:
//
// (1) It returns a matrix of rectangles. That is the results contains
// rows and each row has the same length. So the element at (0, 0) describes
// the first rectangle in the image (top left corner).
//
// (2) A rectangle is not allowed to be empty.
//
// (3) Rectangles might be of different size.
//
// (4) The rectangle is not required to be a part of the image,
// but it must overlap at some point with the image.
//
// (5) The result may be empty (or nil); rows may be empty.
type MosaicArranger interface {
	Divide(image.Image) [][]image.Rectangle
}

type FixedSizeArranger struct {
	Width, Height int
	Cut           bool
}

func NewFixedSizeArranger(width, height int, cut bool) FixedSizeArranger {
	return FixedSizeArranger{Width: width, Height: height, Cut: cut}
}

func (arranger FixedSizeArranger) getSize(originalDimension, tileDimension int) int {
	switch {
	case tileDimension > originalDimension, tileDimension == 0:
		return 1
	case originalDimension%tileDimension == 0:
		return originalDimension / tileDimension
	default:
		if arranger.Cut {
			return originalDimension / tileDimension
		}
		return (originalDimension / tileDimension) + 1
	}
}

// TODO test this (test cases,not just real world)

func (arranger FixedSizeArranger) Divide(img image.Image) [][]image.Rectangle {
	bounds := img.Bounds()
	imgWidth := bounds.Dx()
	imgHeight := bounds.Dy()

	numRows := arranger.getSize(imgHeight, arranger.Height)
	numCols := arranger.getSize(imgWidth, arranger.Width)
	res := make([][]image.Rectangle, numRows)
	for i := 0; i < numRows; i++ {
		res[i] = make([]image.Rectangle, numCols)
		for j := 0; j < numCols; j++ {
			x0 := bounds.Min.X + j*arranger.Width
			y0 := bounds.Min.Y + i*arranger.Height
			x1 := x0 + arranger.Width
			y1 := y0 + arranger.Height
			res[i][j] = image.Rect(x0, y0, x1, y1)
		}
	}
	return res
}

type FixedNumArranger struct {
	NumX, NumY int
	Cut        bool
}

func NewFixedNumArranger(numX, numY int, cut bool) *FixedNumArranger {
	return &FixedNumArranger{NumX: numX, NumY: numY, Cut: cut}
}

func (arranger *FixedNumArranger) Divide(img image.Image) [][]image.Rectangle {
	// kind of a cheat but well
	bounds := img.Bounds()
	imgWidth := bounds.Dx()
	imgHeight := bounds.Dy()

	// some sane defaults if numX or numY should be 0, just to be sure
	width := 1
	height := 1
	if arranger.NumX > 0 {
		width = imgWidth / arranger.NumX
	}
	if arranger.NumY > 0 {
		height = imgHeight / arranger.NumY
	}
	sizeArr := NewFixedSizeArranger(width, height, arranger.Cut)
	return sizeArr.Divide(img)
}

func DivideImage(img image.Image, distribution [][]image.Rectangle) ([][]image.Image, error) {
	bounds := img.Bounds()
	res := make([][]image.Image, len(distribution))
	for i, inner := range distribution {
		res[i] = make([]image.Image, len(inner))
		for j, r := range inner {
			// first intersect tomake sure that we truly have a rectangle in the image
			r = r.Intersect(bounds)
			if r.Empty() {
				// this should not happen
				return nil, fmt.Errorf("Invalid rectangle for mosaic: %v", r)
			}
			// now we try to get the subimage
			subImg, subErr := SubImage(img, r)
			if subErr != nil {
				return nil, fmt.Errorf("Creatin of subimage failed: %v", subErr)
			}
			res[i][j] = subImg
		}
	}
	return res, nil
}
