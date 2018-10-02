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
	_ "image/jpeg"
	_ "image/png"
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

	mapper, mapperErr := gomosaic.CreateFSMapper("/home/fabian/Pictures/mosaic", false, nil)
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
	histogramsConc, histErr := gomosaic.CreateHistograms(storage, true, 8, 8, progress)
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
	log.Println("DONE")
}
