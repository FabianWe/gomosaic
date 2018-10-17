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

package web

import (
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
)

func EncodePNG(image image.Image) (string, error) {
	var w strings.Builder
	encoder := base64.NewEncoder(base64.StdEncoding, &w)
	err := png.Encode(encoder, image)
	if err != nil {
		return "", err
	}
	err = encoder.Close()
	if err != nil {
		return "", err
	}
	s := w.String()
	return s, err
}

func EncodeJPEG(image image.Image, quality int) (string, error) {
	var w strings.Builder
	encoder := base64.NewEncoder(base64.StdEncoding, &w)
	err := jpeg.Encode(encoder, image, &jpeg.Options{Quality: quality})
	if err != nil {
		return "", err
	}
	err = encoder.Close()
	if err != nil {
		return "", err
	}
	s := w.String()
	return s, err
}
