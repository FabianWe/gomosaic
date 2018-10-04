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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	homedir "github.com/mitchellh/go-homedir"
)

var (
	ErrCmdSyntaxErr = errors.New("Invalid command syntax")
)

// TODO this state is rather specific for a file system version,
// maybe some more interfaces will help generalizing?
// But this is stuff for the future when more than images / histograms as
// files are supported

type ExecutorState struct {
	WorkingDir     string
	Mapper         *FSMapper
	ImgStorage     *FSImageDB
	NumRoutines    int
	HistController *HistogramFSController
	Verbose        bool

	In  io.Reader
	Out io.Writer
}

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

type CommandFunc func(state *ExecutorState, args ...string) error

type Command struct {
	Exec        CommandFunc
	Usage       string
	Description string
}

type CommandMap map[string]Command

var DefaultCommands CommandMap

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

func ParseCommand(s string) ([]string, error) {
	parseErr := errors.New("Error parsing command line")
	res := make([]string, 0)
	// basically this is an deterministic automaton, however I can't share my
	// nice image

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

func PwdCommand(state *ExecutorState, args ...string) error {
	_, err := fmt.Fprintln(state.Out, state.WorkingDir)
	return err
}

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

// TODO doc that jpg and png are required
func ImageStorageCommand(state *ExecutorState, args ...string) error {
	var writeErr error
	switch {
	case len(args) == 0:
		_, writeErr = fmt.Fprintln(state.Out, "Number of database images:", state.Mapper.Len())
		return writeErr
	case args[0] == "list":
		for _, path := range state.Mapper.IDMapping {
			_, writeErr = fmt.Fprintf(state.Out, "  %s\n", path)
			if writeErr != nil {
				return writeErr
			}
		}
		_, writeErr = fmt.Fprintln(state.Out, "Total:", state.Mapper.Len())
		return writeErr

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
		_, writeErr = fmt.Fprintln(state.Out, "Loading images from", dir)
		if writeErr != nil {
			return writeErr
		}
		if recursive {
			_, writeErr = fmt.Fprintln(state.Out, "Recursive mode enabled")
			if writeErr != nil {
				return writeErr
			}
		}
		state.Mapper.Clear()
		if loadErr := state.Mapper.Load(dir, recursive, JPGAndPNG); loadErr != nil {
			state.Mapper.Clear()
			return loadErr
		}
		_, writeErr = fmt.Fprintln(state.Out, "Successfully read", state.Mapper.Len(), "images")
		if writeErr != nil {
			return writeErr
		}
		_, writeErr = fmt.Fprintln(state.Out, "Don't forget to (re)load precomputed data if required!")
		return writeErr
	default:
		return ErrCmdSyntaxErr
	}
}

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
		histograms, histErr := CreateAllHistograms(state.ImgStorage,
			true, k, state.NumRoutines, progress)
		if histErr != nil {
			return histErr
		}
		_, writeErr = fmt.Fprintf(state.Out, "Computed %d histograms\n", len(histograms))
		return writeErr
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
		Usage: "gch create [k] or gch TODO",
		Description: "Used to administrate global color histograms (GCHs)\n\n" +
			"If \"create\" is used GCHs are created for all images in the current" +
			"storage. The optional argument k must be a number between 1 and 256." +
			"See usage documentation / Wiki for details about this value. 8 is the" +
			"default value and should be fine.",
	}
}

type ReplHandler struct{}

// TODO doc that it might panic
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
		WorkingDir:     dir,
		NumRoutines:    initialRoutines,
		Mapper:         mapper,
		ImgStorage:     NewFSImageDB(mapper),
		HistController: nil,
		Verbose:        true,
		In:             os.Stdin,
		Out:            os.Stdout,
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
	fmt.Println("Error while executing command:", err.Error())
	return true
}

func (h ReplHandler) OnScanErr(s *ExecutorState, err error) {
	fmt.Println("Error while reading:", err.Error())
}
