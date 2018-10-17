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
	"image/jpeg"
	"log"
	"net/http"
	"os"

	"github.com/FabianWe/gomosaic/web"
)

func bla(dec string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		html := `<html>
			<h1>Bla</h1>
			<img src="data:image/png;base64,%s">
		</html>
		`
		fmt.Fprintf(w, html, dec)

	}
}

func main() {
	memStorage := web.NewMemStorage()
	context := web.NewContext(memStorage)
	// id, _ := web.GenConnectionID()
	// context.Connections.Set(id, web.NewState())
	// done := context.Connections.RunFilter(3*time.Second, time.Second)
	// time.Sleep(10 * time.Second)
	// close(done)
	// time.Sleep(2 * time.Second)

	f, fErr := os.Open("cat.JPG")
	if fErr != nil {
		log.Fatal(fErr)
	}
	defer f.Close()
	img, imgErr := jpeg.Decode(f)
	if imgErr != nil {
		log.Fatal(imgErr)
	}
	dec, decErr := web.EncodePNG(img)
	if decErr != nil {
		log.Fatal(decErr)
	}
	fmt.Println(len(dec))

	web.DefaultHandlers(context, nil)
	http.HandleFunc("/image/", bla(dec))
	log.Fatal(http.ListenAndServe(":8085", nil))
}
