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

package main

import (
	"fmt"
	"image"
	"os"
	"time"

	// These anonymous imports register handlers for jpg and png files, that
	// is the decode method from the image package can now read these files.
	_ "image/jpeg"
	_ "image/png"

	// Since we're not in the gomosaic package we have to import it
	"github.com/FabianWe/gomosaic"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:", os.Args[0], "<IMAGE>")
		os.Exit(1)
	}
	r, openErr := os.Open(os.Args[1])
	if openErr != nil {
		fmt.Println("Can't open file:")
		fmt.Println(openErr)
		os.Exit(1)
	}
	defer r.Close()
	start := time.Now()
	img, _, decodeErr := image.Decode(r)
	if decodeErr != nil {
		fmt.Println("Error parsing image:")
		fmt.Println(decodeErr)
		os.Exit(1)
	}
	histogram := gomosaic.GenHistogram(img, 4)
	execTime := time.Since(start)
	fmt.Println("Histogram:")
	histogram.PrintInfo(true)

	fmt.Println("Normalized histogram:")
	bounds := img.Bounds()

	if bounds.Empty() {
		fmt.Println("No data found")
	} else {
		size := bounds.Dx() * bounds.Dy()
		// size := -1 // computes size automatically
		normalized := histogram.Normalize(size)
		normalized.PrintInfo(true)
		fmt.Printf("Sum of all entries is %.2f\n", normalized.EntrySum())
	}
	fmt.Println("Done after", execTime)
}
