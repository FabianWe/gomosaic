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
	"strconv"
	"strings"
	"time"

	homedir "github.com/mitchellh/go-homedir"
)

var (
	// ErrCmdSyntaxErr is returned by a CommandFunc if the syntax for the command
	// is invalid.
	ErrCmdSyntaxErr = errors.New("Invalid command syntax")
)

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
	// be reloaded.
	GCHStorage *MemoryHistStorage

	// Verbose is true if detailed output should be generated.
	Verbose bool

	// In is the source to read commands from (line by line).
	In io.Reader

	// Out is used to write state information.
	Out io.Writer
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
				// TODO insert error here
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
	_, err := fmt.Fprintln(state.Out, state.WorkingDir)
	return err
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
		if loadErr := state.Mapper.Load(dir, recursive, JPGAndPNG); loadErr != nil {
			state.Mapper.Clear()
			// should not be necessary, just to follow the pattern
			state.GCHStorage = nil
			return loadErr
		}
		fmt.Fprintln(state.Out, "Successfully read", state.Mapper.Len(), "images")
		fmt.Fprintln(state.Out, "Don't forget to (re)load precomputed data if required!")
		return nil
	default:
		return ErrCmdSyntaxErr
	}
}

// TODO doc when finished
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

		var writeErr error
		// create all histograms
		_, writeErr = fmt.Fprintf(state.Out, "Creating histograms for all images in storage with k = %d sub-divisions\n", k)
		if writeErr != nil {
			return writeErr
		}
		var progress ProgressFunc
		if state.Verbose {
			progress = StdProgressFunc(state.Out, "", int(state.ImgStorage.NumImages()), 100)
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
		_, writeErr = fmt.Fprintf(state.Out, "Computed %d histograms in %v\n", len(histograms), execTime)
		return writeErr
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
			fmt.Fprintln(state.Out, "Successfuly wrote", state.ImgStorage.NumImages(), "histograms",
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

func parseGCHMetric(s string) (HistogramMetric, error) {
	var metricName string
	switch {
	case s == "gch":
		metricName = "euclid"
	case strings.HasPrefix(s, "gch-"):
		metricName = s[4:]
	default:
		return nil, fmt.Errorf("Invalid gch format, expect \"gch\" or \"gch-<METRIC>\", got %s", s)
	}
	if metric, ok := GetHistogramMetric(metricName); ok {
		return metric, nil
	}
	return nil, fmt.Errorf("Unkown metric %s", metricName)
}

func saveImage(file string, img image.Image) error {
	outFile, outErr := os.Create(file)
	if outErr != nil {
		return outErr
	}
	defer outFile.Close()
	var encErr error
	ext := filepath.Ext(file)
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		encErr = jpeg.Encode(outFile, img, &jpeg.Options{Quality: 100})
	case ".png":
		encErr = png.Encode(outFile, img)
	default:
		// this should not happen...
		return fmt.Errorf("Unsupported file type: %s, expected .jpg or .png", ext)
	}
	return encErr
}

func MosaicCommand(state *ExecutorState, args ...string) error {
	// TODO test if histograms empty, test if images empty...
	// mosaic in.png out.png gch-... tilesXxtilesY [outDimensions]
	switch {
	case len(args) > 3:
		if !JPGAndPNG(filepath.Ext(args[1])) {
			return fmt.Errorf("Supported files are .jpg and .png, got file %s", args[1])
		}
		// get out path
		outPath, outPathErr := state.GetPath(args[1])
		if outPathErr != nil {
			return outPathErr
		}
		selectionStr := args[2]
		// only gch supported atm
		metric, metricErr := parseGCHMetric(selectionStr)
		if metricErr != nil {
			return metricErr
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
		if state.Verbose {
			fmt.Fprintln(state.Out, "Dividing image into tiles")
		}
		dist := divider.Divide(img.Bounds())
		selector := GCHSelector(state.GCHStorage, metric, state.NumRoutines)
		selection, selectionErr := selector.SelectImages(state.ImgStorage, img, dist)
		if selectionErr != nil {
			return selectionErr
		}
		execTime := time.Since(start)
		if state.Verbose {
			fmt.Fprintln(state.Out, "Preparation took", execTime)
			fmt.Fprintln(state.Out, "Composing mosaic")
		}
		start = time.Now()
		// create mosaic tiles, for this create a new divider and a distribution
		mosaicBounds := image.Rect(0, 0, mosaicWidth, mosaicHeight)
		// TODO make this an option
		// TODO check again with compose if this is okay
		// TODO check again all requirements
		divider.Cut = false
		mosaicDist := divider.Divide(mosaicBounds)
		// TODO resizer should be an option
		mosaic, mosaicErr := ComposeMosaic(state.ImgStorage, selection, mosaicDist,
			DefaultResizer, ForceResize)
		if mosaicErr != nil {
			return mosaicErr
		}
		execTime = time.Since(start)
		if state.Verbose {
			fmt.Fprintln(state.Out, "Image selection took", execTime)
			fmt.Fprintln(state.Out, "Saving image")
		}
		if writeErr := saveImage(outPath, mosaic); writeErr != nil {
			return writeErr
		}
		fmt.Fprintln(state.Out, "Mosaic saved to", outPath)
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
	DefaultCommands["cd"] = Command{
		Exec:        CdCommand,
		Usage:       "cd <DIR>",
		Description: "Change working directory to the specified directory",
	}
	DefaultCommands["storage"] = Command{
		Exec:  ImageStorageCommand,
		Usage: "storage [list] or storage load [DIR]",
		Description: "This command controls the images that are considered" +
			"database images. This does not mean that all these images have some" +
			"precomputed data, like histograms. Only that they were found as" +
			"possible images. You have to use other commands to load precomputed" +
			"data.\n\nIf \"list\" is used a list of all images will be printed" +
			"note that this can be quite large\n\n" +
			"if load is used the image storage will be initialized with images from" +
			"the directory (working directory if no image provided). All previously" +
			"loaded images will be removed from the storage.",
	}
	DefaultCommands["gch"] = Command{
		Exec:  GCHCommand,
		Usage: "gch create [k] or gch load <FILE> or gch save <FILE>",
		Description: "Used to administrate global color histograms (GCHs)\n\n" +
			"If \"create\" is used GCHs are created for all images in the current" +
			"storage. The optional argument k must be a number between 1 and 256." +
			"See usage documentation / Wiki for details about this value. 8 is the" +
			"default value and should be fine.\n\nsave and load commands load files" +
			"containing GHCs from a file.",
	}
	DefaultCommands["mosaic"] = Command{
		Exec:        MosaicCommand,
		Usage:       "TODO",
		Description: "TODO",
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
		WorkingDir:  dir,
		NumRoutines: initialRoutines,
		Mapper:      mapper,
		ImgStorage:  NewFSImageDB(mapper),
		GCHStorage:  nil,
		Verbose:     true,
		In:          os.Stdin,
		Out:         os.Stdout,
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
