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
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/nfnt/resize"
)

var (
	// ErrCmdSyntaxErr is returned by a CommandFunc if the syntax for the command
	// is invalid.
	ErrCmdSyntaxErr = errors.New("Invalid command syntax")
)

type cmdVarietySelector int

const (
	cmdVarietyNone cmdVarietySelector = iota
	cmdVarietyRand
)

func (s cmdVarietySelector) displayString() string {
	switch s {
	case cmdVarietyNone:
		return "None"
	case cmdVarietyRand:
		return "Random"
	default:
		return "Unkown"
	}
}

func parseCMDVarietySelector(s string) (cmdVarietySelector, error) {
	switch strings.ToLower(s) {
	case "none":
		return cmdVarietyNone, nil
	case "random":
		return cmdVarietyRand, nil
	default:
		return -1, fmt.Errorf("Unkown variety type: %s", s)
	}
}

// TODO this state is rather specific for a file system version,
// maybe some more interfaces will help generalizing?
// But this is stuff for the future when more than images / histograms as
// files are supported

// ExecutorState is the state during a CommandHandler execution, see that
// type for more details of the workflow.
//
// The variables in the state are shared among the executions of the command
// functions.
type ExecutorState struct {
	// WorkingDir is the current directory. It must always be an absolute path.
	WorkingDir string

	// Mapper is the current file system mapper.
	Mapper *FSMapper

	// ImgStorage is image database, backed by Mapper.
	ImgStorage *FSImageDB

	// NumRoutines is the number of go routines used for different tasks during
	// mosaic generation.
	NumRoutines int

	// GCHStorage stores the global color histograms. Whenever new images are
	// loaded the old histograms become invalid (set to nil again) and must
	// be reloaded / created.
	GCHStorage *MemoryHistStorage

	// LCHStorage stores the local color histograms. Whenever new images are
	// loaded the old histograms become invalid (set to nil again) and must
	// be reloaded / created.
	LCHStorage *MemoryLCHStorage

	// Verbose is true if detailed output should be generated.
	Verbose bool

	// In is the source to read commands from (line by line).
	In io.Reader

	// Out is used to write state information.
	Out io.Writer

	// Option / config part

	// CutMosaic describes whether the mosaic should be "cut".
	// Cutting means to cut the resulting image s.t. each tile has the same bounds.
	// Example: Suppose you want to divide an image with width 99 and want ten
	// tiles horizontally. This leads to an image where each tile has
	// a width of 9. Ten tiles yields to a final width of 90. As you see 9 pixels
	// are "left over". The distribution in ten tiles is fixed, so we can't add
	// another tile. But in order to enforce the original proposed width
	// we can enlarge the last tile by 9 pixels. So we would have 9 tiles with
	// width 9 and one tile with width 18.
	//
	// Cut controls what to do with those remaining pixels: If cut is set
	// to true we skip the 9 pixels and return an image of size 90. If set to
	// false we enlarge the last tile and return an image with size 99.
	// Usually the default is false.
	CutMosaic bool

	// JPGQuality is the quality between 1 and 100 used when storing images.
	// The higher the value the better the quality. We use a default quality of
	// 100.
	JPGQuality int

	// InterP is the interpolation functions used when resizing the images.
	InterP resize.InterpolationFunction

	// Cache size is the size of the image cache during mosaic composition.
	// The more elements in the cache the faster the composition process is, but
	// it also increases memory consumption. If cache size is < 0 the
	// DefaultCacheSize is used.
	CacheSize int

	// VarietySelector is the current variety selector, defaults to
	// cmdVarietyNone.
	VarietySelector cmdVarietySelector

	// BestFit is the percent value (between 0 and 1) that describes how much
	// percent of the input images are considered in the variety heaps.
	BestFit float64
}

// GetPath returns the absolute path given some other path.
// The idea is the following: If the user inputs a path we have two cases:
// The user used an absolute path, in this case we use this absolute path
// to perform tasks with.
// If it is a relative path we join the working directory with this path
// and thus retrieve the absolute path we work on.
//
// The home directory can be used like on Unix: ~/Pictures is the Pictures
// directory in the home directory of the user.
func (state *ExecutorState) GetPath(path string) (string, error) {
	var res string
	// first extend with homedir
	var pathErr error
	res, pathErr = homedir.Expand(path)
	if pathErr != nil {
		return "", pathErr
	}
	// now we test if we have an absolute path or a relative path.
	// if absolute path we don't need to do anything.
	// if relative path we have to join with the base directory
	if !filepath.IsAbs(res) {
		// join with base dir
		res = filepath.Join(state.WorkingDir, res)
	}
	// now convert to an absolute path again
	res, pathErr = filepath.Abs(res)
	if pathErr != nil {
		return "", pathErr
	}
	return res, nil
}

// GetBestFitImages multiplies that best fit factor (BestFit) with num images
// to get the number of best fit images for the variety selectors. It sets
// same sane defaults in the case something weird happens.
func (state *ExecutorState) GetBestFitImages(numImages int) int {
	asFloat := float64(numImages) * state.BestFit
	asInt := int(asFloat)
	return IntMin(IntMax(asInt, 1), numImages)
}

// CommandFunc is a function that is applied to the current states and
// arguments to that command.
type CommandFunc func(state *ExecutorState, args ...string) error

// Command a command consists of a function to actually execute the command
// and some information about the command.
type Command struct {
	Exec        CommandFunc
	Usage       string
	Description string
}

// CommandMap maps command names to Commands.
type CommandMap map[string]Command

// DefaultCommands contains some commands that are often used.
var DefaultCommands CommandMap

// CommandHandler together with Execute implements a high-level command
// execution loop. CommandFuncs are applied to the current state until there
// are no more commands to execute (no more input).
//
// We won't go into the details, please read the source for details (yes,
// that's probably not the best practise but is so much easier in this case).
//
// A command has the form "COMMAND ARG1 ... ARGN" where COMMAND is the command
// name and ARG1 to ARGN are the arguments for the command.
//
// Here's a rough summary of what Execute will do:
// First it creates an initial state by calling Init. After that it immediately
// calls Start to notify the handler that the execution begins.
//
// We use to different methods to separate object creation from execution.
// Before a command is executed the Before method is called to notify the
// handler that a command will be executed.
//
// Then a loop will begin that reads all lines from the state's reader.
// If there is a command line the line will be parsed, if an error during
// parsing occurred the handler gets notified via OnParseErr. This method
// should return true if the execution should continue despite the error.
// Then a lookup in the provided command man happens: If the command was
// found the corresponding Command object is executed. If it was not found
// the OnInvalidCmd function is called on the handler. Again it should return
// true if the exeuction should continue despite the error. If this execution
// was successful the OnSuccess function is called with the executed command.
// If the execution was unsuccessful the OnError function will be called.
// Commands should return ErrCmdSyntaxErr if the syntax of the command is
// incorrect (for example invalid number of arguments) and OnError can do
// special handling in this case. Again OnError returns true if execution should
// continue.
// OnScanErr is called if there is an error while reading a command line from
// the state's reader.
//
// Errors while writing to the provided out stream might be reported, but
// this is not a requirement.
type CommandHandler interface {
	Init() *ExecutorState
	Start(s *ExecutorState)
	Before(s *ExecutorState)
	After(s *ExecutorState)
	OnParseErr(s *ExecutorState, err error) bool
	OnInvalidCmd(s *ExecutorState, cmd string) bool
	OnSuccess(s *ExecutorState, cmd Command)
	OnError(s *ExecutorState, err error, cmd Command) bool
	OnScanErr(s *ExecutorState, err error)
}

// Execute implements the high-level execution loop as described in the
// documentation of CommandHandler. commandMap is used to lookup commands.
func Execute(handler CommandHandler, commandMap CommandMap) {
	state := handler.Init()
	handler.Start(state)
	scanner := bufio.NewScanner(state.In)
	for scanner.Scan() {
		// a bit ugly with the calls to After:
		// we want something like deferring in the loop...
		handler.Before(state)
		line := scanner.Text()
		parsedCmd, parseErr := ParseCommand(line)
		if parseErr != nil {
			if !handler.OnParseErr(state, parseErr) {
				return
			}
			handler.After(state)
			continue
		}
		if len(parsedCmd) == 0 {
			handler.After(state)
			continue
		}
		cmd := parsedCmd[0]
		if nextCmd, ok := commandMap[cmd]; ok {
			// try to execute
			if execErr := nextCmd.Exec(state, parsedCmd[1:]...); execErr == nil {
				// execution of command was a success
				handler.OnSuccess(state, nextCmd)
			} else {
				// execution of command failed
				if !handler.OnError(state, execErr, nextCmd) {
					return
				}
				// continue with next
				handler.After(state)
				continue
			}
		} else {
			// we got an invalid command
			if !handler.OnInvalidCmd(state, cmd) {
				return
			}
			// continue with next command
			handler.After(state)
			continue
		}
		handler.After(state)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		handler.OnScanErr(state, scanErr)
	}
}

func isEOF(r []rune, i int) bool {
	return i == len(r)
}

// ParseCommand parses a command of the form "COMMAND ARG1 ... ARGN".
// Examples:
//
// foo bar is the command "foo" with argument "bar". Arguments might also
// be enclosed in quotes, so foo "bar bar" is parsed as command foo with
// argument bar bar (a single argument).
func ParseCommand(s string) ([]string, error) {
	parseErr := errors.New("Error parsing command line")
	res := make([]string, 0)
	// basically this is an deterministic automaton, however I can't share my
	// "nice" image with you

	// the following 3 variables mean:
	// state is the state of the automaton, we have 5 states
	// i is the index in the position in s in which to apply the state function
	// start is the start of the argument (when we're finished parsing one)
	// however, we don't work on the string but on runes
	r := []rune(s)
	state, i := 0, 0
	// while parsing runes get appended here to build the current argument
	currentArg := make([]rune, 0)
	// now iterate over each rune
L:
	for ; i <= len(r); i++ {
		switch state {
		case 0:
			// state when we parse a new command, that means currentArg must be empty
			if isEOF(r, i) {
				// done parsing
				break L
			}
			switch r[i] {
			case ' ':
				// do nothing, just remain in state
			case '\\':
				state = 2
			case '"':
				state = 3
			default:
				currentArg = append(currentArg, r[i])
				state = 1
			}
		case 1:
			// state where we parse an argument not enclosed in ""
			if isEOF(r, i) {
				break L
			}
			switch r[i] {
			case ' ':
				// parsing done
				res = append(res, string(currentArg))
				currentArg = nil
				state = 0
			case '\\':
				state = 2
			case '"':
				return nil, parseErr
			default:
				//remain in state, append rune
				currentArg = append(currentArg, r[i])
			}
		case 2:
			// state where we previously parsed a \, so know we must parse either "
			// \
			if isEOF(r, i) {
				return nil, parseErr
			}
			switch r[i] {
			case '\\', '"':
				// add to current arg and switch back to state 1
				currentArg = append(currentArg, r[i])
				state = 1
			default:
				return nil, parseErr
			}
		case 3:
			// state where we parse an argument enclosed in ""
			if isEOF(r, i) {
				return nil, parseErr
			}
			switch r[i] {
			case '"':
				// parsing done
				res = append(res, string(currentArg))
				currentArg = nil
				state = 0
			case '\\':
				state = 4
			default:
				currentArg = append(currentArg, r[i])
			}
		case 4:
			// state where we previously parsed a \, so know we must parse either "
			// \
			// Similar to state 2, but know we reached the state from an arg
			// enclosed in ""
			if isEOF(r, i) {
				return nil, parseErr
			}
			switch r[i] {
			case '\\', '"':
				// add to current arg and switch back to state 3
				currentArg = append(currentArg, r[i])
				state = 3
			default:
				return nil, parseErr
			}
		}
	}
	// now something might still be there (just a break in the loop, not adding
	// to res)
	if len(currentArg) > 0 {
		res = append(res, string(currentArg))
	}
	return res, nil
}

// PwdCommand is a command that prints the current working directory.
func PwdCommand(state *ExecutorState, args ...string) error {
	fmt.Fprintln(state.Out, state.WorkingDir)
	return nil
}

// StatsCommand is a command that prints variable / value pairs.
func StatsCommand(state *ExecutorState, args ...string) error {
	m := map[string]interface{}{
		"routines":     state.NumRoutines,
		"verbose":      state.Verbose,
		"cut":          state.CutMosaic,
		"jpeg-quality": state.JPGQuality,
		"interp":       InterPString(state.InterP),
		"cache":        state.CacheSize,
		"variety":      state.VarietySelector.displayString(),
		"best":         fmt.Sprintf("%.2f %%", 100.0*state.BestFit),
	}
	if len(args) == 1 {
		// print specific value
		if val, has := m[args[0]]; has {
			fmt.Fprintf(state.Out, "%s ==> %v\n", args[0], val)
		} else {
			return fmt.Errorf("Unkown variable %s", args[0])
		}
	} else {
		// print all values
		// keep order deterministic
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, variable := range keys {
			val := m[variable]
			fmt.Fprintf(state.Out, "%s ==> %v\n", variable, val)
		}
	}
	return nil
}

// SetVarCommand sets a variable to a new value.
func SetVarCommand(state *ExecutorState, args ...string) error {
	if len(args) != 2 {
		return errors.New("Invalid set syntax: Requires variable and value. For a list of variables use \"stats\"")
	}
	name, valueStr := args[0], args[1]
	switch name {
	case "routines":
		val, parseErr := strconv.Atoi(valueStr)
		if parseErr != nil {
			return fmt.Errorf("Invalid value for routines (must be positive int): %s", parseErr.Error())
		}
		if val <= 0 {
			return fmt.Errorf("Invalid value for routines (must be positive int): %d", val)
		}
		state.NumRoutines = val
		return nil
	case "verbose":
		val, parseErr := strconv.ParseBool(valueStr)
		if parseErr != nil {
			return fmt.Errorf("Invalid value for verbose (must be true or false): %s", parseErr.Error())
		}
		state.Verbose = val
		return nil
	case "cut":
		val, parseErr := strconv.ParseBool(valueStr)
		if parseErr != nil {
			return fmt.Errorf("Invalid value for cut (must be true or false): %s", parseErr.Error())
		}
		state.CutMosaic = val
		return nil
	case "jpeg-quality":
		val, parseErr := strconv.Atoi(valueStr)
		if parseErr != nil {
			return fmt.Errorf("Invalid value for jpeg-quality (must be int between 1 and 100): %s", parseErr.Error())
		}
		if val < 1 || val > 100 {
			return fmt.Errorf("Invalid value for jpeg-quality (must be int between 1 and 100): %d", val)
		}
		state.JPGQuality = val
		return nil
	case "interp":
		val, parseErr := strconv.Atoi(valueStr)
		if parseErr != nil {
			return fmt.Errorf("Invalid value for interpolation function, must be integer >= 0: %s", parseErr.Error())
		}
		if val < 0 {
			return fmt.Errorf("Invalid value for interpolation function, must be integer >= 0: %d", val)
		}
		interP := GetInterP(uint(val))
		state.InterP = interP
		return nil
	case "cache":
		val, parseErr := strconv.Atoi(valueStr)
		if parseErr != nil {
			return fmt.Errorf("Invalid value for cache size, must be an integer: %d", val)
		}
		state.CacheSize = val
		return nil
	case "variety":
		val, parseErr := parseCMDVarietySelector(valueStr)
		if parseErr != nil {
			return fmt.Errorf("Invalid value for variety, must be \"None\" or \"Random\", got: \"%s\"", valueStr)
		}
		state.VarietySelector = val
		return nil
	case "best":
		val, parseErr := ParsePercent(valueStr)
		if parseErr != nil {
			return fmt.Errorf("Invalid value for best, must be a percent (50.0%% or 0.5), got %s", valueStr)
		}
		state.BestFit = val
		return nil
	default:
		return fmt.Errorf("Invalid variable \"%s\". For a list use \"stats\"", name)
	}
}

// CdCommand is a command that changes the current directory.
func CdCommand(state *ExecutorState, args ...string) error {
	if len(args) != 1 {
		return ErrCmdSyntaxErr
	}
	path := args[0]
	var expandErr error
	path, expandErr = homedir.Expand(path)
	if expandErr != nil {
		return fmt.Errorf("Changing directory failed: %s", expandErr.Error())
	}
	if fi, err := os.Lstat(path); err != nil {
		return fmt.Errorf("Changing directory failed: %s", err.Error())
	} else {
		if !fi.IsDir() {
			return fmt.Errorf("Changing directory failed: \"%s\" is not a directory", path)
		} else {
			// convert to absolute path
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				return fmt.Errorf("Changing directory failed: %s", absErr.Error())
			} else {
				state.WorkingDir = abs
				return nil
			}
		}
	}
}

// ImageStorageCommand is a command that executes tasks with the fs mapper
// and therefor the image storage (the user doesn't need to know about details
// as mapper and storage, so it's simply called storage).
// This command without arguments just prints the number of databases in the
// storage.
// With the single argument "list" it prints the path of each image in the
// storage.
// With the argument "load" a second argument "DIR" is required, this will
// load all images from the directory in the storage. If a third argument
// is provided this must be a bool that is true if the directory should be
// scanned recursively. The default is not to scan recursively.
//
// Note that jpg and png files are considered valid image types, thus
// image.jpeg and image.png should be included if you're planning to use
// this function.
func ImageStorageCommand(state *ExecutorState, args ...string) error {
	switch {
	case len(args) == 0:
		fmt.Fprintln(state.Out, "Number of database images:", state.Mapper.Len())
		return nil
	case args[0] == "list":
		for _, path := range state.Mapper.IDMapping {
			fmt.Fprintf(state.Out, "  %s\n", path)
		}
		fmt.Fprintln(state.Out, "Total:", state.Mapper.Len())
		return nil
	case args[0] == "load":
		var dir string
		var recursive bool

		switch {
		case len(args) == 1:
			dir = state.WorkingDir
		case len(args) > 2:
			// parse recursive flag
			var boolErr error
			recursive, boolErr = strconv.ParseBool(args[2])
			if boolErr != nil {
				return boolErr
			}
			// parse path argument
			fallthrough
		case len(args) > 1:
			// parse the path
			var pathErr error
			dir, pathErr = state.GetPath(args[1])
			if pathErr != nil {
				return pathErr
			}
		default:
			// just to be sure, should never happen
			return nil
		}
		fmt.Fprintln(state.Out, "Loading images from", dir)
		if recursive {
			fmt.Fprintln(state.Out, "Recursive mode enabled")
		}
		state.Mapper.Clear()
		// make gchs invalid
		state.GCHStorage = nil
		// make lchs invalid
		state.LCHStorage = nil
		if loadErr := state.Mapper.Load(dir, recursive, JPGAndPNG); loadErr != nil {
			state.Mapper.Clear()
			// should not be necessary, just to follow the pattern
			state.GCHStorage = nil
			state.LCHStorage = nil
			return loadErr
		}
		fmt.Fprintln(state.Out, "Successfully read", state.Mapper.Len(), "images")
		fmt.Fprintln(state.Out, "Don't forget to (re)load precomputed data if required!")
		return nil
	default:
		return ErrCmdSyntaxErr
	}
}

// TODO stuff here should be moved to other functions to avoid repeating code
// later...

// GCHCommand can create histograms for all images in storage, save and load
// files.
func GCHCommand(state *ExecutorState, args ...string) error {
	switch {
	case len(args) == 0:
		return ErrCmdSyntaxErr
	case args[0] == "create":
		// k is the number of subdivions, defaults to 8
		var k uint = 8
		if len(args) > 1 {
			asInt, parseErr := strconv.Atoi(args[1])
			if parseErr != nil {
				return parseErr
			}
			// validate k: must be >= 1 and <= 256
			if asInt < 1 || asInt > 256 {
				return fmt.Errorf("k for GCH must be a value between 1 and 256, got %d", asInt)
			}
			k = uint(asInt)
		}

		// create all histograms
		fmt.Fprintf(state.Out, "Creating histograms for all images in storage with k = %d sub-divisions\n", k)
		var progress ProgressFunc
		if state.Verbose {
			inStore := int(state.ImgStorage.NumImages())
			progress = StdProgressFunc(state.Out, "",
				inStore, IntMin(100, inStore/10))
		}
		start := time.Now()
		histograms, histErr := CreateAllHistograms(state.ImgStorage,
			true, k, state.NumRoutines, progress)
		execTime := time.Since(start)
		if histErr != nil {
			return histErr
		}
		// set histograms
		state.GCHStorage = &MemoryHistStorage{Histograms: histograms, K: k}
		fmt.Fprintf(state.Out, "Computed %d histograms in %v\n", len(histograms), execTime)
		return nil
	case args[0] == "save":
		if state.GCHStorage == nil {
			return errors.New("No GCHs loaded yet")
		}
		// save ~/bla.[json|gob]
		// save ~/
		if len(args) < 2 {
			return ErrCmdSyntaxErr
		}
		path, pathErr := state.GetPath(args[1])
		if pathErr != nil {
			return pathErr
		}
		// check if path is a file or directory
		// we don't report the fiErr (this is not nil if file doesn't exist which
		// is of course allowed)
		fi, fiErr := os.Lstat(path)
		if fiErr == nil && fi.IsDir() {
			// save with default naming scheme in that directory
			name := GCHFileName(state.GCHStorage.K, "gob")
			path = filepath.Join(path, name)
		}
		controller, creationErr := CreateHistFSController(IDList(state.ImgStorage),
			state.Mapper, state.GCHStorage)
		if creationErr != nil {
			return creationErr
		}
		// save file
		saveErr := controller.WriteFile(path)
		if saveErr == nil {
			// ignore write error here
			fmt.Fprintln(state.Out, "Successfully wrote", state.ImgStorage.NumImages(), "histograms",
				"to", path)
		}
		return saveErr
	case args[0] == "load":
		if len(args) < 2 {
			return ErrCmdSyntaxErr
		}
		path, pathErr := state.GetPath(args[1])
		if pathErr != nil {
			return pathErr
		}
		controller := HistogramFSController{}
		readErr := controller.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		fmt.Fprintf(state.Out, "Read %d histograms\n", len(controller.Entries))
		// we don't care about missing / new images, we just print a warning if
		// the lengths have change.
		if len(controller.Entries) != int(state.ImgStorage.NumImages()) {
			fmt.Fprintln(state.Out, "Unmatched number of images in storage and loaded histograms.",
				"Have the images changed? In this case the histograms must be re-computed.")
		}
		memStorage, createErr := MemHistStorageFromFSMapper(state.Mapper, &controller, nil)
		if createErr != nil {
			return createErr
		}
		state.GCHStorage = memStorage
		fmt.Fprintln(state.Out, "Histograms have been mapped to image store.")
		return nil
	default:
		return ErrCmdSyntaxErr
	}
}

func LCHCommand(state *ExecutorState, args ...string) error {
	switch {
	case len(args) == 0:
		return ErrCmdSyntaxErr
	case args[0] == "create":
		if len(args) < 3 {
			return ErrCmdSyntaxErr
		}
		// k is the number of subdivions
		asInt, parseErr := strconv.Atoi(args[1])
		if parseErr != nil {
			return parseErr
		}
		// validate k: must be >= 1 and <= 256
		if asInt < 1 || asInt > 256 {
			return fmt.Errorf("k for LCH must be a value between 1 and 256, got %d", asInt)
		}
		k := uint(asInt)
		// parse scheme size
		asInt, parseErr = strconv.Atoi(args[2])
		if parseErr != nil {
			return parseErr
		}
		// now create lch scheme
		var scheme LCHScheme
		switch asInt {
		case 4:
			scheme = NewFourLCHScheme()
		case 5:
			scheme = NewFiveLCHScheme()
		default:
			return fmt.Errorf("Invalid scheme size %d: Supported are 4 and 5", asInt)
		}
		// create all lchs
		fmt.Fprintf(state.Out, "Creating LCHs for all images in storage with k = %d sub-divisions and %d parts\n", k, asInt)
		var progress ProgressFunc
		if state.Verbose {
			inStore := int(state.ImgStorage.NumImages())
			progress = StdProgressFunc(state.Out, "",
				inStore, IntMin(100, inStore/10))
		}
		start := time.Now()
		lchs, lchsErr := CreateAllLCHs(scheme, state.ImgStorage,
			true, k, state.NumRoutines, progress)
		execTime := time.Since(start)
		if lchsErr != nil {
			return lchsErr
		}
		// set
		state.LCHStorage = &MemoryLCHStorage{
			LCHs: lchs,
			K:    k,
			Size: uint(asInt),
		}
		fmt.Fprintf(state.Out, "Computed %d LCHs in %v\n", len(lchs), execTime)
		return nil
	case args[0] == "save":
		if state.LCHStorage == nil {
			return errors.New("No LCHs loaded yet")
		}
		if len(args) < 2 {
			return ErrCmdSyntaxErr
		}
		path, pathErr := state.GetPath(args[1])
		if pathErr != nil {
			return pathErr
		}
		// check if path is a file or directory
		// we don't report the fiErr (this is not nil if file doesn't exist which
		// is of course allowed)
		fi, fiErr := os.Lstat(path)
		if fiErr == nil && fi.IsDir() {
			// save with default naming scheme in that directory
			name := LCHFileName(state.LCHStorage.K, state.LCHStorage.Size, "gob")
			path = filepath.Join(path, name)
		}
		controller, creationErr := CreateLCHFSController(IDList(state.ImgStorage),
			state.Mapper, state.LCHStorage)
		if creationErr != nil {
			return creationErr
		}
		// save file
		saveErr := controller.WriteFile(path)
		if saveErr == nil {
			fmt.Fprintln(state.Out, "Successfully wrote", state.ImgStorage.NumImages(),
				"LCHs to", path)
		}
		return saveErr
	case args[0] == "load":
		if len(args) < 2 {
			return ErrCmdSyntaxErr
		}
		path, pathErr := state.GetPath(args[1])
		if pathErr != nil {
			return pathErr
		}
		controller := LCHFSController{}
		readErr := controller.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		fmt.Fprintf(state.Out, "Read %d LCHs\n", len(controller.Entries))
		// we don't care about missing / new images, we just print a warning if
		// the lengths have change.
		if len(controller.Entries) != int(state.ImgStorage.NumImages()) {
			fmt.Fprintln(state.Out, "Unmachted number of images in storage and loaded",
				"LCHs. Have the images changed? In this case the LCHs must be re-computed.")
		}
		memStorage, createErr := MemLCHStorageFromFSMapper(state.Mapper, &controller, nil)
		if createErr != nil {
			return createErr
		}
		// set
		state.LCHStorage = memStorage
		fmt.Fprintln(state.Out, "LCHs have been mapped to image store.")
		return nil
	default:
		return ErrCmdSyntaxErr
	}
}

func parseGCHMetric(s string) (HistogramMetric, error) {
	var metricName string
	switch {
	case s == "gch":
		metricName = "euclid"
	case strings.HasPrefix(s, "gch-"):
		metricName = s[4:]
	default:
		return nil, fmt.Errorf("Invalid gch format, expect \"gch\" or \"gch-<metric>\", got %s", s)
	}
	if metric, ok := GetHistogramMetric(metricName); ok {
		return metric, nil
	}
	return nil, fmt.Errorf("Unkown metric %s", metricName)
}

func parseLCHMetric(s string) (HistogramMetric, error) {
	var metricName string
	switch {
	case s == "lch":
		metricName = "euclid"
	case strings.HasPrefix(s, "lch-"):
		metricName = s[4:]
	default:
		return nil, fmt.Errorf("Invalid lch format, expect \"lch\" or \"lch-<metric>\", got %s", s)
	}
	if metric, ok := GetHistogramMetric(metricName); ok {
		return metric, nil
	}
	return nil, fmt.Errorf("Unkown metric %s", metricName)
}

func saveImage(file string, img image.Image, jpgQuality int) error {
	outFile, outErr := os.Create(file)
	if outErr != nil {
		return outErr
	}
	defer outFile.Close()
	var encErr error
	ext := filepath.Ext(file)
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		encErr = jpeg.Encode(outFile, img, &jpeg.Options{Quality: jpgQuality})
	case ".png":
		encErr = png.Encode(outFile, img)
	default:
		// this should not happen...
		return fmt.Errorf("Unsupported file type: %s, expected .jpg or .png", ext)
	}
	return encErr
}

// MosaicCommand creates a mosaic images.
// For details see the entry created in the init() method / the description
// text of the command our the online documentation. Usage example:
// mosaic in.jpg out.jpg gch-cosine 20x30 1024x768
func MosaicCommand(state *ExecutorState, args ...string) error {
	// mosaic in.png out.png gch-... tilesXxtilesY [outDimensions]
	if int(state.ImgStorage.NumImages()) == 0 {
		return errors.New("No images in storage, use \"storage load\"")
	}
	switch {
	case len(args) > 3:
		totalStart := time.Now()
		if !JPGAndPNG(filepath.Ext(args[1])) {
			return fmt.Errorf("Supported files are .jpg and .png, got file %s", args[1])
		}
		// get out path
		outPath, outPathErr := state.GetPath(args[1])
		if outPathErr != nil {
			return outPathErr
		}

		selectionStr := args[2]
		// supported gch and lch
		useGCH := true

		// try to parse gch and lch
		// not so nice, we compute prefix stuff later again... but well
		switch {
		case strings.HasPrefix(selectionStr, "gch"):
			useGCH = true
			if state.GCHStorage == nil {
				return errors.New("No GCH data loaded, use \"gch create\" or \"gch load\"")
			}
		case strings.HasPrefix(selectionStr, "lch"):
			useGCH = false
			if state.LCHStorage == nil {
				return errors.New("No LCH data loaded, use \"lch create\" or \"lch load\"")
			}
		default:
			return fmt.Errorf("Invalid image selector, expected gch or lch, got %s", selectionStr)
		}

		tilesX, tilesY, tilesParseErr := ParseDimensions(args[3])
		if tilesParseErr != nil {
			return ErrCmdSyntaxErr
		}
		if tilesX == 0 || tilesY == 0 {
			return fmt.Errorf("Tiles dimensions are not allowed to be empty, got %s", args[3])
		}
		inPath, inPathErr := state.GetPath(args[0])
		if inPathErr != nil {
			return inPathErr
		}
		// read query image
		if state.Verbose {
			fmt.Fprintln(state.Out, "Reading image", inPath)
		}
		start := time.Now()
		r, openErr := os.Open(inPath)
		if openErr != nil {
			return openErr
		}
		defer r.Close()
		img, _, decodeErr := image.Decode(r)
		if decodeErr != nil {
			return decodeErr
		}
		queryBounds := img.Bounds()
		if queryBounds.Empty() {
			return errors.New("Query image is empty")
		}
		queryWidth, queryHeight := queryBounds.Dx(), queryBounds.Dy()
		// compute output dimensions now that we have the original image
		var mosaicWidth, mosaicHeight int
		if len(args) > 4 {
			var mosaicParseErr error
			mosaicWidth, mosaicHeight, mosaicParseErr = ParseDimensionsEmpty(args[4])
			if mosaicParseErr != nil {
				return mosaicParseErr
			}
			// because dimensions are allowed to be empty we have to deal with
			// negative values
			switch {
			case mosaicWidth < 0 && mosaicHeight < 0:
				// keep original size
				mosaicWidth, mosaicHeight = queryWidth, queryHeight
			case mosaicWidth < 0:
				// compute width and keep ratio
				mosaicWidth = KeepRatioWidth(queryWidth, queryHeight, mosaicHeight)
			case mosaicHeight < 0:
				// compute height and keep ratio
				mosaicHeight = KeepRatioHeight(queryWidth, queryHeight, mosaicWidth)
			default:
				// do nothing, both given
			}
		} else {
			mosaicWidth, mosaicHeight = queryWidth, queryHeight
		}
		if mosaicWidth == 0 || mosaicHeight == 0 {
			return fmt.Errorf("Mosaic image would be empty, dimensions %dx%d", mosaicWidth, mosaicHeight)
		}
		divider := NewFixedNumDivider(tilesX, tilesY, true)
		dist := divider.Divide(img.Bounds())
		var selector ImageSelector
		if useGCH {
			metric, metricErr := parseGCHMetric(selectionStr)
			if metricErr != nil {
				return metricErr
			}
			switch state.VarietySelector {
			case cmdVarietyNone:
				selector = GCHSelector(state.GCHStorage, metric, state.NumRoutines)
			case cmdVarietyRand:
				imageMetric := NewHistogramImageMetric(state.GCHStorage, metric, state.NumRoutines)
				numBestFit := state.GetBestFitImages(int(state.ImgStorage.NumImages()))
				selector = RandomHeapImageSelector(imageMetric, numBestFit, state.NumRoutines)
			default:
				return fmt.Errorf("Internal error, please report bug: Got unkown variety selector (GCH): %d", state.VarietySelector)
			}
		} else {
			metric, metricErr := parseLCHMetric(selectionStr)
			if metricErr != nil {
				return metricErr
			}
			// TODO this fixes the scheme on the number, that is no other four or
			// five part scheme can be used, but I guess that's just fine
			// otherwise we must safe it somewhere
			var scheme LCHScheme
			switch state.LCHStorage.SchemeSize() {
			case 4:
				scheme = NewFourLCHScheme()
			case 5:
				scheme = NewFiveLCHScheme()
			default:
				// should never happen
				return fmt.Errorf("Invalid scheme with %d parts. This is a bug! Pleas report", state.LCHStorage.SchemeSize())
			}
			switch state.VarietySelector {
			case cmdVarietyNone:
				selector = LCHSelector(state.LCHStorage, scheme, metric, state.NumRoutines)
			case cmdVarietyRand:
				imageMetric := NewLCHImageMetric(state.LCHStorage, scheme, metric, state.NumRoutines)
				numBestFit := state.GetBestFitImages(int(state.ImgStorage.NumImages()))
				selector = RandomHeapImageSelector(imageMetric, numBestFit, state.NumRoutines)
			default:
				return fmt.Errorf("Internal error, please report bug: Got unkown variety selector (LCH): %d", state.VarietySelector)
			}
		}
		if state.Verbose {
			fmt.Fprintln(state.Out)
			fmt.Fprintln(state.Out, "Selecting database images for tiles")
		}
		var progress ProgressFunc
		if state.Verbose {
			numTiles := dist.Size()
			progress = StdProgressFunc(state.Out, "",
				numTiles, IntMin(100, numTiles/10))
		}
		selection, selectionErr := selector.SelectImages(state.ImgStorage, img, dist, progress)
		if selectionErr != nil {
			return selectionErr
		}
		execTime := time.Since(start)
		if state.Verbose {
			fmt.Fprintln(state.Out, "Selection took", execTime)
			fmt.Fprintln(state.Out)
			fmt.Fprintln(state.Out, "Composing mosaic")
		}
		start = time.Now()
		// create mosaic tiles, for this create a new divider and a distribution
		mosaicBounds := image.Rect(0, 0, mosaicWidth, mosaicHeight)
		divider.Cut = state.CutMosaic
		mosaicDist := divider.Divide(mosaicBounds)
		// progress func should be fine to use
		mosaic, mosaicErr := ComposeMosaic(state.ImgStorage, selection, mosaicDist,
			NewNfntResizer(state.InterP), ForceResize, state.NumRoutines, ImageCacheSize, progress)
		if mosaicErr != nil {
			return mosaicErr
		}
		execTime = time.Since(start)
		if state.Verbose {
			fmt.Fprintln(state.Out, "Composition of mosaic took took", execTime)
			fmt.Fprintln(state.Out)
			fmt.Fprintln(state.Out, "Saving image")
		}
		if writeErr := saveImage(outPath, mosaic, state.JPGQuality); writeErr != nil {
			return writeErr
		}
		fmt.Fprintln(state.Out, "Mosaic saved to", outPath)
		if state.Verbose {
			totalTime := time.Since(totalStart)
			fmt.Fprintln(state.Out)
			fmt.Fprintln(state.Out, "Total creation time:", totalTime)
		}
		return nil
	default:
		return ErrCmdSyntaxErr
	}
}

func init() {
	DefaultCommands = make(map[string]Command, 20)
	DefaultCommands["pwd"] = Command{
		Exec:        PwdCommand,
		Usage:       "pwd",
		Description: "Show current working directory.",
	}
	DefaultCommands["stats"] = Command{
		Exec:        StatsCommand,
		Usage:       "stats [var]",
		Description: "Show value of variables that can be changed via set, if var is given only value of that variable",
	}
	DefaultCommands["set"] = Command{
		Exec:  SetVarCommand,
		Usage: "set <variable> <value>",
		Description: "Set value for a variable. For details about the variables" +
			" please refer to the user documentation.",
	}
	DefaultCommands["cd"] = Command{
		Exec:        CdCommand,
		Usage:       "cd <dir>",
		Description: "Change working directory to the specified directory",
	}
	DefaultCommands["storage"] = Command{
		Exec:  ImageStorageCommand,
		Usage: "storage [list] or storage load [dir]",
		Description: "This command controls the images that are considered" +
			" database images. This does not mean that all these images have some" +
			" precomputed data, like histograms. Only that they were found as" +
			" possible images. You have to use other commands to load precomputed" +
			" data.\n\nIf \"list\" is used a list of all images will be printed" +
			" note that this can be quite large\n\n" +
			"If load is used the image storage will be initialized with images from" +
			" the directory (working directory if no image provided). All previously" +
			" loaded images will be removed from the storage.",
	}
	DefaultCommands["gch"] = Command{
		Exec:  GCHCommand,
		Usage: "gch create [k] or gch load <file> or gch save <file>",
		Description: "Used to administrate global color histograms (GCHs)\n\n" +
			"If \"create\" is used GCHs are created for all images in the current" +
			" storage. The optional argument k must be a number between 1 and 256." +
			" See usage documentation / Wiki for details about this value. 8 is the" +
			" default value and should be fine.\n\nsave and load commands load files" +
			" containing GHCs from a file.",
	}
	DefaultCommands["lch"] = Command{
		Exec:  LCHCommand,
		Usage: "lch create <k> <scheme> or lch load <file> or lch save <file>",
		Description: "Used to administrate local color histograms (LCHs)\n\n" +
			"\"crate\", \"load\" and \"save\" work as in the gch command. k is also" +
			"the same as in the GCH command and scheme is the number of GCHs created" +
			"for each image (must be either 4 or 5).",
	}
	DefaultCommands["mosaic"] = Command{
		Exec:  MosaicCommand,
		Usage: "mosaic <in> <out> <metric> <tiles> [dimension]",
		Description: "Creates a mosaic based on global color histograms (GCHs)." +
			" in is the path to the query image, out the path to the output image" +
			" (i.e. mosaic), metric is of the form gch-metric, e.g. gch-cosine." +
			" a list of supported metrics is given below. tiles describes the number" +
			" of tiles to use in the mosaic, for example \"30x20\" creates 30 times 20" +
			" tiles (30 in x and 20 in y direction). dimension is optional a describes" +
			" the dimensions of the output image. If omitted the dimensions of the input" +
			" are used. For example 1024x768 creates a mosaic with 1024 width and 768" +
			" height. A value can be omitted and the ratio of the query image is retained." +
			" \"1024x\" means a mosaic with width 1024 and the height is computed by" +
			" the query ratio. Also works in the other direction like \"x768\".\n\n" +
			"Example Usage: \"mosaic in.jpg out.jpg gch-cosine 20x30 1024x768\". Valid" +
			" metrics (each with prefix \"gch-\" like \"gch-cosine\"):\n\n" +
			strings.Join(GetHistogramMetricNames(), " "),
	}
}

// ReplHandler implements CommandHandler by reading commands from stdin and
// writing output to stdout.
type ReplHandler struct{}

// Init creates an initial ExecutorState. It creates a new mapper and
// image database and sets the working directory to the current directory.
// This method might panic if something with filepath is wrong, this should
// however usually not be the case.
func (h ReplHandler) Init() *ExecutorState {
	// seems reasonable
	initialRoutines := runtime.NumCPU() * 2
	if initialRoutines <= 0 {
		// don't know if this can happen, better safe then sorry
		initialRoutines = 4
	}
	dir, err := filepath.Abs(".")
	if err != nil {
		panic(fmt.Errorf("Unable to retrieve path: %s", err.Error()))
	}
	mapper := NewFSMapper()
	return &ExecutorState{
		// dir is always an absolute path
		WorkingDir:      dir,
		NumRoutines:     initialRoutines,
		Mapper:          mapper,
		ImgStorage:      NewFSImageDB(mapper),
		GCHStorage:      nil,
		LCHStorage:      nil,
		Verbose:         true,
		In:              os.Stdin,
		Out:             os.Stdout,
		CutMosaic:       false,
		JPGQuality:      100,
		InterP:          resize.Lanczos3,
		CacheSize:       ImageCacheSize,
		VarietySelector: cmdVarietyNone,
		BestFit:         0.05,
	}
}

func (h ReplHandler) Start(s *ExecutorState) {
	fmt.Println("Welcome to the gomosaic generator")
	fmt.Println("Copyright © 2018 Fabian Wenzelmann")
	fmt.Print(">>> ")
}

func (h ReplHandler) Before(s *ExecutorState) {}

func (h ReplHandler) After(s *ExecutorState) {
	fmt.Print(">>> ")
}

func (h ReplHandler) OnParseErr(s *ExecutorState, err error) bool {
	fmt.Println("Syntax error", err)
	return true
}

func (h ReplHandler) OnInvalidCmd(s *ExecutorState, cmd string) bool {
	fmt.Printf("Invalid command \"%s\"\n", cmd)
	return true
}

func (h ReplHandler) OnSuccess(s *ExecutorState, cmd Command) {}

func (h ReplHandler) OnError(s *ExecutorState, err error, cmd Command) bool {
	if err == ErrCmdSyntaxErr {
		fmt.Println("Invalid syntax for command.")
		fmt.Println("Usage:", cmd.Usage)
	} else {
		fmt.Println("Error while executing command:", err.Error())
	}
	return true
}

func (h ReplHandler) OnScanErr(s *ExecutorState, err error) {
	fmt.Println("Error while reading:", err.Error())
}

// ScriptHandler implements CommandHandler. It writes the output to stdout
// and reads from a specified reader. It stops whenever an error is enountered.
type ScriptHandler struct {
	Source io.Reader
}

// NewScriptHandler returns a new script handler that reads input from the given
// source.
func NewScriptHandler(source io.Reader) ScriptHandler {
	return ScriptHandler{Source: source}
}

// Init creates an initial ExecutorState. It creates a new mapper and
// image database and sets the working directory to the current directory.
// This method might panic if something with filepath is wrong, this should
// however usually not be the case.
func (h ScriptHandler) Init() *ExecutorState {
	// seems reasonable
	initialRoutines := runtime.NumCPU() * 2
	if initialRoutines <= 0 {
		// don't know if this can happen, better safe then sorry
		initialRoutines = 4
	}
	dir, err := filepath.Abs(".")
	if err != nil {
		panic(fmt.Errorf("Unable to retrieve path: %s", err.Error()))
	}
	mapper := NewFSMapper()
	return &ExecutorState{
		// dir is always an absolute path
		WorkingDir:      dir,
		NumRoutines:     initialRoutines,
		Mapper:          mapper,
		ImgStorage:      NewFSImageDB(mapper),
		GCHStorage:      nil,
		LCHStorage:      nil,
		Verbose:         true,
		In:              h.Source,
		Out:             os.Stdout,
		CutMosaic:       false,
		JPGQuality:      100,
		InterP:          resize.Lanczos3,
		CacheSize:       ImageCacheSize,
		VarietySelector: cmdVarietyNone,
		BestFit:         0.05,
	}
}

func (h ScriptHandler) Start(s *ExecutorState) {}

func (h ScriptHandler) Before(s *ExecutorState) {}

func (h ScriptHandler) After(s *ExecutorState) {}

func (h ScriptHandler) OnParseErr(s *ExecutorState, err error) bool {
	fmt.Fprintln(os.Stderr, "Syntax error:", err)
	return false
}

func (h ScriptHandler) OnInvalidCmd(s *ExecutorState, cmd string) bool {
	fmt.Fprintf(os.Stderr, "Invalid command \"%s\"\n", cmd)
	return false
}

func (h ScriptHandler) OnSuccess(s *ExecutorState, cmd Command) {}

func (h ScriptHandler) OnError(s *ExecutorState, err error, cmd Command) bool {
	if err == ErrCmdSyntaxErr {
		fmt.Fprintln(os.Stderr, "Error: Invalid syntax for command.")
		fmt.Fprintln(os.Stderr, "Usage:", cmd.Usage)
	} else {
		fmt.Fprintln(os.Stderr, "Error while executing command:", err.Error())
	}
	return false
}

func (h ScriptHandler) OnScanErr(s *ExecutorState, err error) {
	fmt.Fprintln(os.Stderr, "Error while reading:", err.Error())
}

// ScriptHandlerFromCmds is a function to create a script handler from
// a predefined set of lines. This allows us for easy execution of predefined
// scripts.
func ScriptHandlerFromCmds(lines []string) ScriptHandler {
	return NewScriptHandler(ReaderFromCmdLines(lines))
}

// ReaderFromCmdLines returns a reader for a script source that reads the
// content of the combined lines.
func ReaderFromCmdLines(lines []string) io.Reader {
	combined := strings.Join(lines, "\n")
	return strings.NewReader(combined)
}

// Parameterized is used to transform parameterized commands into executable
// commands, that means replacing variables $i with the provided argument.
// Example:
// The command "gch load $1" can be called with one argument that will replace
// the placeholder $1.
// It should work with an arbitrary number of variables (let's hope so).
//
// The current implementation works by reading the whole original reader and
// then transforming the elements, given that scripts are not too long the
// overhead should be manageable.
//
// If no parameters are given it is best practise to avoid calling this method
// and use the original reader.
func Parameterized(r io.Reader, args ...string) (io.Reader, error) {
	// create replacer that replaces each $i by args[i-1]
	replaceArgs := make([]string, 0, 2*len(args))
	for i := len(args) - 1; i >= 0; i-- {
		replaceArgs = append(replaceArgs, fmt.Sprintf("$%d", i+1), args[i])
	}
	replacer := strings.NewReplacer(replaceArgs...)
	lines := make([]string, 0, 20)
	// iterate over each line and perform replacement
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		line = replacer.Replace(line)
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ReaderFromCmdLines(lines), nil
}

// ParameterizedFromStrings runs the commands provided in commands (each entry
// is considered to be a command) and replaces placeholders by args.
// For placeholder details see Parameterized.
func ParameterizedFromStrings(commands []string, args ...string) io.Reader {
	// create replacer that replaces each $i by args[i-1]
	replaceArgs := make([]string, 0, 2*len(args))
	for i := len(args) - 1; i >= 0; i-- {
		replaceArgs = append(replaceArgs, fmt.Sprintf("$%d", i+1), args[i])
	}
	replacer := strings.NewReplacer(replaceArgs...)
	lines := make([]string, 0, len(commands))
	// iterate over each line and perform replacement
	for _, line := range lines {
		line = replacer.Replace(line)
		lines = append(lines, line)
	}
	return ReaderFromCmdLines(lines)
}
