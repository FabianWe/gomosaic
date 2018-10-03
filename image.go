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
	"image/color"
	"reflect"
	"strings"

	"github.com/nfnt/resize"
)

// SupportedImageFunc is a function that takes a file extension and decides if
// this file extension is supported. Usually our library should support jpg
// and png files, but this may change depending on what image protocols are
// loaded.
//
// The extension passed to this function could be for example ".txt" or ".jpg".
// JPGAndPNG is an implementation accepting jpg and png files.
type SupportedImageFunc func(ext string) bool

// JPGAndPNG is an implementation of SupportedImageFunc accepting jpg and png
// file extensions.
func JPGAndPNG(ext string) bool {
	ext = strings.ToLower(ext)
	switch ext {
	case ".jpg", ".jpeg", ".png":
		return true
	default:
		return false
	}
}

const (
	// QuantizeFactor is used during quantiation, it's the number of values in
	// each rgb component.
	QuantizeFactor uint = 256
)

// QuantizeC quantizes the color component c (sub-divison in k values).
// That is it returns c * (k / 256). k must be a number between 1 and 256.
func QuantizeC(val uint8, k uint) uint8 {
	return uint8((uint(val) * k) / QuantizeFactor)
}

// RGBID assigns each RGB color (k sub-divisions) a unique id. That id is given
// by r + k * g + k² * b.
//
// RGBs Id method uses this function, it's just more convenient if the id can
// be computed without creating an RGB object.
func RGBID(r, g, b, k uint) uint {
	return r + k*g + k*k*b
}

// RGB is a color containing r, g and b components.
type RGB struct {
	R, G, B uint8
}

// NewRGB returns a new RGB color.
func NewRGB(r, g, b uint8) RGB {
	return RGB{R: r, G: g, B: b}
}

// ConvertRGB converts a generic color into the internal RGB representation.
func ConvertRGB(c color.Color) RGB {
	// convert to rgba model
	rgba := color.RGBAModel.Convert(c).(color.RGBA)
	// convert to internal rgb representation
	return RGB{R: rgba.R, G: rgba.G, B: rgba.B}
}

// ID assigns each RGB color (k sub-divisions) a unique id. That id is given
// by r + k * g + k² * b.
func (c RGB) ID(k uint) uint {
	return RGBID(uint(c.R), uint(c.G), uint(c.B), k)
}

// Quantize quantizes the RGB color (k sub-divisions in each direction).
// k must be a number between 1 and 256.
func (c RGB) Quantize(k uint) RGB {
	return RGB{
		R: QuantizeC(c.R, k),
		G: QuantizeC(c.G, k),
		B: QuantizeC(c.B, k)}
}

// SubImager is a type that can produce a sub image from an original image.
type SubImager interface {
	SubImage(r image.Rectangle) image.Image
}

// SubImage returns a subimage of img given the boundaries r.
// The rectangle should be a valid area in the image. If the image type does
// not have a sub image method an error is returned.
func SubImage(img image.Image, r image.Rectangle) (image.Image, error) {
	imager, ok := img.(SubImager)
	if !ok {
		return nil, fmt.Errorf("Can't create sub image from type %v", reflect.TypeOf(img))
	}
	return imager.SubImage(r), nil
}

// TODO maybe it is a good idea to add thumbnail as in nfnt?

// ImageResizer resizes an image to the given width and height.
type ImageResizer interface {
	Resize(width, height uint, img image.Image) image.Image
}

// NfntResizer uses the nfnt/resize package to resize an image.
type NfntResizer struct {
	// InterP is the interpolation function to use.
	InterP resize.InterpolationFunction
}

// NewNfntResizer returns a new resizer given the interpolation function.
func NewNfntResizer(interP resize.InterpolationFunction) NfntResizer {
	return NfntResizer{interP}
}

// GetInterP returns an interpolation function given a desired quality.
// The higher the quality the better the interpolation should be, but execution
// time is higher. Currently supported are values between 0 and 4, each
// selecting a different interpolation function. Values greater than 4 are
// treated as 4.
//
// This method assumes that the interpolation functions provided by nfnt/resize
// can be sorted according to their quality. This should be a reasonable
// assumption.
func GetInterP(quality uint) resize.InterpolationFunction {
	switch quality {
	case 0:
		return resize.NearestNeighbor
	case 1:
		return resize.Bilinear
	case 2:
		return resize.Bicubic
	case 3:
		return resize.MitchellNetravali
	case 4:
		return resize.Lanczos2
	default:
		return resize.Lanczos3
	}
}

var (
	// DefaultResizer is the resizer that is used by default, if you're
	// looking for a resizer default argument this seems useful.
	DefaultResizer = NewNfntResizer(resize.MitchellNetravali)
)

// Resize calls nfnt/resize methods.
func (resizer NfntResizer) Resize(width, height uint, img image.Image) image.Image {
	return resize.Resize(width, height, img, resizer.InterP)
}

// ImageID is used to unambiguously identify an image.
type ImageID int

const (
	// NoImageID is used to signal errors etc. on images.
	// It is usually never used and you don't have to care about ImageID < 0.
	// Certain functions however use this value in a specific way.
	NoImageID ImageID = -1
)

// ImageStorage is used to administrate a collection or database of images.
// Images are not stored in memory but are identified by an id and can be loaded
// into memory when required.
// A storage has a maximal id and can be used to access images with ids smaller
// than the the number of images.
// The access methods should return an error if the image id is not associated
// with any image data or if there is an error reading the image (e.g. from
// the filesystem).
//
// Implementations must be safe for concurrent use.
type ImageStorage interface {
	// NumImages returns the number of images in the storage as an ImageID.
	// All ids < than NumImages are considered valid and can be retrieved via
	// LoadImage.
	NumImages() ImageID

	// LoadImage loads an image into memory.
	LoadImage(id ImageID) (image.Image, error)

	// LoadConfig loads the config of the image with the given id.
	LoadConfig(id ImageID) (image.Config, error)
}

// IDList returns the list [0, 1, ..., storage.NumImages - 1].
func IDList(storage ImageStorage) []ImageID {
	numImages := storage.NumImages()
	res := make([]ImageID, numImages)
	var i ImageID
	for ; i < numImages; i++ {
		res[i] = i
	}
	return res
}
