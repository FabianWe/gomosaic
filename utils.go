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
	"io"
	"strconv"
	"strings"

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
		if step == 0 {
			return
		}
		if !(step < 0 || num%step == 0) {
			return
		}
		if max == 0 {
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

// StdProgressFunc is a parameterized ProgressFunc that logs to the
// specified writer.
// The output describes the progress (how many of how many objects processed).
// Log messages may have an addition prefix. max is the total number of elements
// to process and step describes how often to print to the log (for example
// step = 100 every 100 items).
func StdProgressFunc(w io.Writer, prefix string, max, step int) ProgressFunc {
	return func(num int) {
		if step == 0 {
			return
		}
		if !(step < 0 || num%step == 0) {
			return
		}
		if max == 0 {
			return
		}
		percent := (float64(num) / float64(max)) * 100.0
		if percent > 100.0 {
			percent = 100.0
		}
		if prefix == "" {
			fmt.Printf("Progress: %d of %d (%.1f%%)\n", num, max, percent)
		} else {
			fmt.Printf("%s: %d of %d (%.1f%%)\n", prefix, num, max, percent)
		}
	}
}

// ParseDimensions parses a string of the form "AxB" where A and B are positive
// integers.
func ParseDimensions(s string) (int, int, error) {
	split := strings.Split(s, "x")
	if len(split) != 2 {
		return -1, -1, fmt.Errorf("Invalid dimension format: %s. Expect \"AxB\"", s)
	}
	first, second := strings.TrimSpace(split[0]), strings.TrimSpace(split[1])
	firstInt, firstErr := strconv.Atoi(first)
	if firstErr != nil {
		return -1, -1, firstErr
	}
	secondInt, secondErr := strconv.Atoi(second)
	if secondErr != nil {
		return -1, -1, secondErr
	}
	if firstInt < 0 || secondInt < 0 {
		return -1, -1, fmt.Errorf("Dimensions must be positive, got %d and %d",
			firstInt, secondInt)
	}
	return firstInt, secondInt, nil
}

// ParseDimensionsEmpty works as ParseDimensions of the form "AxB" but also
// also A and / or B to be empty. That is "1024x" would be valid as well as
// "x768" and "x". Empty values are returned as -1.
func ParseDimensionsEmpty(s string) (int, int, error) {
	split := strings.Split(s, "x")
	if len(split) != 2 {
		return -1, -1, fmt.Errorf("Invalid dimension format: %s. Expect \"AxB\"", s)
	}
	first, second := strings.TrimSpace(split[0]), strings.TrimSpace(split[1])
	firstInt, secondInt := -1, -1
	var parseErr error
	// now parse both ints, but only if not empty
	if len(first) > 0 {
		firstInt, parseErr = strconv.Atoi(first)
		if parseErr != nil {
			return -1, -1, parseErr
		}
		if firstInt < 0 {
			return -1, -1, fmt.Errorf("Dimensions must be positive, got %d", firstInt)
		}
	}

	if len(second) > 0 {
		secondInt, parseErr = strconv.Atoi(second)
		if parseErr != nil {
			return -1, -1, parseErr
		}
		if secondInt < 0 {
			return -1, -1, fmt.Errorf("Dimensions must be positive, got %d", secondInt)
		}
	}
	return firstInt, secondInt, nil
}

// KeepRatioHeight computes the new height given the original width and height
// s.t. the ration remains unchanged. The original values must be > 0.
func KeepRatioHeight(originalWidth, originalHeight, width int) int {
	ratio := float64(originalHeight) / float64(originalWidth)
	return int(ratio * float64(width))
}

// KeepRatioWidth computes the new width given the original width and height
// s.t. the ration remains unchanged. The original values must be > 0.
func KeepRatioWidth(originalWidth, originalHeight, height int) int {
	ratio := float64(originalWidth) / float64(originalHeight)
	return int(ratio * float64(height))
}
