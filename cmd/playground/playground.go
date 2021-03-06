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
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"time"
	// Since we're not in the gomosaic package we have to import it
	"github.com/FabianWe/gomosaic"

	log "github.com/sirupsen/logrus"
)

func init() {
	if gomosaic.Debug {
		log.SetLevel(log.DebugLevel)
	}
}

func main() {

	path := "/home/fabian/Pictures/test"
	mapper, mapperErr := gomosaic.CreateFSMapper(path, true, nil)
	if mapperErr != nil {
		log.Fatal(mapperErr)
	}
	storage := gomosaic.NewFSImageDB(mapper)
	progress := gomosaic.LoggerProgressFunc("gen-hist", int(storage.NumImages()), 100)
	fmt.Printf("Creating histograms for %d images\n", storage.NumImages())
	start := time.Now()
	histograms, histErr := gomosaic.CreateHistogramsSequential(storage, true, 8, progress)
	if histErr != nil {
		log.Fatal(histErr)
	}
	execTime := time.Since(start)
	fmt.Printf("Computed %d histograms after %v\n", len(histograms), execTime)

	fmt.Printf("Creating histograms for %d images concurrently\n", storage.NumImages())
	start = time.Now()
	histogramsConc, histErr := gomosaic.CreateAllHistograms(storage, true, 8, 8, progress)
	if histErr != nil {
		log.Fatal(histErr)
	}
	execTime = time.Since(start)
	fmt.Printf("Computed %d histograms after %v\n", len(histogramsConc), execTime)

	if len(histograms) != len(histogramsConc) {
		log.Fatal("Weird, histograms not of same length")
	}
	for i, h := range histograms {
		other := histogramsConc[i]
		if !h.Equals(other, 0.01) {
			log.Fatal("Not equal...")
		}
	}
	// TODO not the nicest way to use it
	histStorage := &gomosaic.MemoryHistStorage{Histograms: histogramsConc, K: 8}
	fsController, controllerErr := gomosaic.CreateHistFSController(gomosaic.IDList(storage), mapper, histStorage)
	if controllerErr != nil {
		log.Fatal(controllerErr)
	}
	writeErr := fsController.WriteGobFile(filepath.Join(path, "hists.gob"))
	if writeErr != nil {
		log.Fatal(writeErr)
	}
	foo := &gomosaic.HistogramFSController{}
	readErr := foo.ReadGobFile(filepath.Join(path, "hists.gob"))
	if readErr != nil {
		log.Fatal(readErr)
	}
	writeErr = fsController.WriteJSON(filepath.Join(path, "hists.json"))
	if writeErr != nil {
		log.Fatal(writeErr)
	}
	bar := &gomosaic.HistogramFSController{}
	readErr = bar.ReadJSONFile(filepath.Join(path, "hists.json"))
	if readErr != nil {
		log.Fatal(readErr)
	}

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
	img, _, decodeErr := image.Decode(r)
	if decodeErr != nil {
		fmt.Println("Error parsing image:")
		fmt.Println(decodeErr)
		os.Exit(1)
	}

	hMetric, _ := gomosaic.GetHistogramMetric("cosine")

	fmt.Println("Composing mosaic")
	start = time.Now()
	selector := gomosaic.GCHSelector(histStorage, hMetric, 8)
	selector.Init(storage)
	div := gomosaic.NewFixedNumDivider(20, 30, false)
	dist := div.Divide(img.Bounds())
	comp, compseErr := selector.SelectImages(storage, img, dist, nil)
	execTime = time.Since(start)
	if compseErr != nil {
		log.Fatal(compseErr)
	}
	fmt.Println("Done after", execTime)
	// compose mosaic
	fmt.Println("Composing mosaic image")
	mosaic, mosaicErr := gomosaic.ComposeMosaic(storage, comp, dist,
		gomosaic.DefaultResizer, gomosaic.ForceResize, 8, -1, nil)
	execTime = time.Since(start)
	if mosaicErr != nil {
		log.Fatal(mosaicErr)
	}
	fmt.Println("Done after", execTime)
	outFile, outErr := os.Create("mosaic.png")
	if outErr != nil {
		log.Fatal(outErr)
	}
	defer outFile.Close()
	encErr := png.Encode(outFile, mosaic)
	if encErr != nil {
		log.Fatal(encErr)
	}
	fmt.Println("Composing with random")
	var sel gomosaic.ImageSelector
	sel = gomosaic.RandomHeapImageSelector(gomosaic.NewHistogramImageMetric(histStorage, hMetric, 8),
		2, 8)
	if initErr := sel.Init(storage); initErr != nil {
		log.Fatal(initErr)
	}
	comp, compseErr = sel.SelectImages(storage, img, dist, nil)
	if compseErr != nil {
		log.Fatal(compseErr)
	}
	mosaic, mosaicErr = gomosaic.ComposeMosaic(storage, comp, dist, gomosaic.DefaultResizer,
		gomosaic.ForceResize, 8, -1, nil)
	if mosaicErr != nil {
		log.Fatal(mosaicErr)
	}
	outFile, outErr = os.Create("mosaic_rand.png")
	if outErr != nil {
		log.Fatal(outErr)
	}
	defer outFile.Close()
	encErr = png.Encode(outFile, mosaic)
	if encErr != nil {
		log.Fatal(encErr)
	}
}

func main2() {
	path := "/home/fabian/Pictures/test"
	mapper, mapperErr := gomosaic.CreateFSMapper(path, true, nil)
	if mapperErr != nil {
		log.Fatal(mapperErr)
	}
	storage := gomosaic.NewFSImageDB(mapper)
	progress := gomosaic.LoggerProgressFunc("gen-hist", int(storage.NumImages()), 100)
	fmt.Printf("Creating histograms for %d images\n", storage.NumImages())
	start := time.Now()
	histograms, histErr := gomosaic.CreateHistogramsSequential(storage, true, 8, progress)
	if histErr != nil {
		log.Fatal(histErr)
	}
	execTime := time.Since(start)
	fmt.Printf("Computed %d histograms after %v\n", len(histograms), execTime)

	fmt.Printf("Creating histograms for %d images concurrently\n", storage.NumImages())
	start = time.Now()
	histogramsConc, histErr := gomosaic.CreateAllHistograms(storage, true, 8, 8, progress)
	if histErr != nil {
		log.Fatal(histErr)
	}
	execTime = time.Since(start)
	fmt.Printf("Computed %d histograms after %v\n", len(histogramsConc), execTime)

	if len(histograms) != len(histogramsConc) {
		log.Fatal("Weird, histograms not of same length")
	}
	for i, h := range histograms {
		other := histogramsConc[i]
		if !h.Equals(other, 0.01) {
			log.Fatal("Not equal...")
		}
	}
	// TODO not the nicest way to use it
	histStorage := &gomosaic.MemoryHistStorage{Histograms: histogramsConc, K: 8}
	fsController, controllerErr := gomosaic.CreateHistFSController(gomosaic.IDList(storage), mapper, histStorage)
	if controllerErr != nil {
		log.Fatal(controllerErr)
	}
	writeErr := fsController.WriteGobFile(filepath.Join(path, "hists.gob"))
	if writeErr != nil {
		log.Fatal(writeErr)
	}
	foo := &gomosaic.HistogramFSController{}
	readErr := foo.ReadGobFile(filepath.Join(path, "hists.gob"))
	if readErr != nil {
		log.Fatal(readErr)
	}
	writeErr = fsController.WriteJSON(filepath.Join(path, "hists.json"))
	if writeErr != nil {
		log.Fatal(writeErr)
	}
	bar := &gomosaic.HistogramFSController{}
	readErr = bar.ReadJSONFile(filepath.Join(path, "hists.json"))
	if readErr != nil {
		log.Fatal(readErr)
	}

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
	img, _, decodeErr := image.Decode(r)
	if decodeErr != nil {
		fmt.Println("Error parsing image:")
		fmt.Println(decodeErr)
		os.Exit(1)
	}

	hMetric, _ := gomosaic.GetHistogramMetric("cosine")

	fmt.Println("Composing mosaic")
	start = time.Now()
	selector := gomosaic.GCHSelector(histStorage, hMetric, 8)
	selector.Init(storage)
	div := gomosaic.NewFixedNumDivider(20, 30, false)
	dist := div.Divide(img.Bounds())
	comp, compseErr := selector.SelectImages(storage, img, dist, nil)
	execTime = time.Since(start)
	if compseErr != nil {
		log.Fatal(compseErr)
	}
	fmt.Println("Done after", execTime)
	// compose mosaic
	fmt.Println("Composing mosaic image")
	mosaic, mosaicErr := gomosaic.ComposeMosaic(storage, comp, dist,
		gomosaic.DefaultResizer, gomosaic.ForceResize, 8, -1, nil)
	execTime = time.Since(start)
	if mosaicErr != nil {
		log.Fatal(mosaicErr)
	}
	fmt.Println("Done after", execTime)
	outFile, outErr := os.Create("mosaic.png")
	if outErr != nil {
		log.Fatal(outErr)
	}
	defer outFile.Close()
	encErr := png.Encode(outFile, mosaic)
	if encErr != nil {
		log.Fatal(encErr)
	}
}
