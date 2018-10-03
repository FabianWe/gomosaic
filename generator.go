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

import "image"

// MosaicArranger is a type to divide an image into tiles. That is it creates
// the areas which should be replaced by images from the database.
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
// (4) The rectangle ust not be a part of the image, but it must overlap at
// some point with the image.
type MosaicArranger interface {
	Divide(image.Image) [][]image.Rectangle
}
