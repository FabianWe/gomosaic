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
	log "github.com/sirupsen/logrus"
)

const (
	// Debug is true if code should be compiled in debug mode, printing
	// more stuff and performing checks.
	Debug = true
)

var (
	// BufferSize is the (default) size of buffers. Some methods create buffered
	// channels, this parameter controls how big such buffers might be.
	// Usually such buffers store no big data (ints, bools etc.).
	BufferSize = 1000
)

// ProgressFunc is a function that is used to inform a caller about the progress
// of a called function.
// For example if we process thousands of images we might wish to know
// how far the call is and give feedback to the user.
// The called method calls the process function after each iteration.
type ProgressFunc func(num int)

// ProgressIgnore is a ProgressFunc that does nothing.
func ProgressIgnore(num int) {}

// LoggerProgressFunc is a parameterized ProgressFunc that logs to log.
// The output describes the progress (how many of how many objects processed).
// Log messages may have an addition prefix. max is the total number of elements
// to process and step describes how often to print to the log (for example
// step = 100 every 100 items).
func LoggerProgressFunc(prefix string, max, step int) ProgressFunc {
	return func(num int) {
		if !(step < 0 || num%step == 0) {
			return
		}
		percent := (float64(num) / float64(max)) * 100.0
		if percent > 100.0 {
			percent = 100.0
		}
		if prefix == "" {
			log.Printf("Progress: %d of %d (%.1f%%)", num, max, percent)
		} else {
			log.Printf("%s: %d of %d (%.1f%%)", prefix, num, max, percent)
		}
	}
}
