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

// This file contains some predefined scripts that can be executed. This way
// we have some easy way to crate mosaics without requiring the user to know
// any details.

var (
	// RunSimple contains script code that when executed loads images from memory,
	// creates all GCHs and then creates the mosaic. No GCHs are stored on the
	// filesystem.
	// It is parameterized by five parameters: First the directory containing the
	// database images, second the name of the input file, third the name of the
	// output file fourth the number of tiles in the mosaic and last the
	// dimensions of the output. Because the last element can actually be omitted
	// we can create a mosaic with just four parameters.
	//
	// It is at the moment the easiest way to create a mosaic.
	// But it can be very slow if the database is quite large.
	//
	// Example usage: RunSimple ~/Pictures/ input.jpg output.png 20x30 x
	//
	// This would create output.png with 20x30 tiles from input.jpg with images
	// from ~/Pictures/. The output image would have the same size as the input
	// image (no dimension given).
	RunSimple = `storage load $1
gch create
mosaic $2 $3 gch-euclid $4 $5`

	// RunMetric is similar to RunSimple but takes an additional argument: The
	// metric name.
	//
	// Example usage: RunMetric ~/Pictures/ input.jpg output.png 20x30 x cosine
	//
	// This would do the same as RunSimple but using cosine-similarity.
	RunMetric = `storage load $1
gch create
mosaic $2 $3 gch-$6 $4 $5`

	// CompareMetrics is similar to RunSimple but generates multiple output
	// images based on different metrics. Thus the third argument is not an path
	// for a file but a directory. In this directory multiple mosaics will be
	// generated.
	//
	// Example usage: CompareMetrics ~/Pictures/ input.jpg ./output/ 20x30 x
	CompareMetrics = `storage load $1
gch create
mosaic $2 $3/mosaic-manhattan.jpg gch-manhattan $4 $5
mosaic $2 $3/mosaic-euclid.jpg gch-euclid $4 $5
mosaic $2 $3/mosaic-min.jpg gch-min $4 $5
mosaic $2 $3/mosaic-cosine.jpg gch-cosine $4 $5
mosaic $2 $3/mosaic-chessboard.jpg gch-chessboard $4 $5
mosaic $2 $3/mosaic-canberra.jpg gch-canberra $4 $5`
)
