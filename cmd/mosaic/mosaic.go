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
	"bufio"
	"errors"
	"fmt"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	// Since we're not in the gomosaic package we have to import it
	"github.com/FabianWe/gomosaic"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
)

func main() {
	// seems reasonable
	initialRoutines := runtime.NumCPU() * 2
	if initialRoutines <= 0 {
		// don't know if this can happen, better safe then sorry
		initialRoutines = 4
	}
	dir, err := filepath.Abs(".")
	if err != nil {
		fmt.Println("Error: Unable to retrieve path:", err)
		os.Exit(1)
	}
	if len(os.Args) == 1 {
		mapper := gomosaic.NewFSMapper()
		repl(&ExecutorState{
			// dir is always an absolute path
			WorkingDir:     dir,
			NumRoutines:    initialRoutines,
			Mapper:         mapper,
			ImgStorage:     gomosaic.NewFSImageDB(mapper),
			HistController: nil,
			Verbose:        true,
		})
	}
}

type ExecutorState struct {
	WorkingDir     string
	Mapper         *gomosaic.FSMapper
	ImgStorage     *gomosaic.FSImageDB
	NumRoutines    int
	HistController *gomosaic.HistogramFSController
	Verbose        bool
}

type CommandFunc func(state *ExecutorState, args ...string) bool

type Command struct {
	exec        CommandFunc
	usage       string
	description string
}

func isEOF(r []rune, i int) bool {
	return i == len(r)
}

func parseCommand(s string) ([]string, error) {
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

var commandMap map[string]Command

func init() {
	if gomosaic.Debug {
		log.SetLevel(log.DebugLevel)
	}

	commandMap = make(map[string]Command, 20)
	commandMap["help"] = Command{
		exec:        helpCommand,
		usage:       "help",
		description: "Show help.",
	}
	commandMap["exit"] = Command{
		exec:        exitCommand,
		usage:       "exit",
		description: "Exit the program.",
	}
	commandMap["pwd"] = Command{
		exec:        pwdCommand,
		usage:       "pwd",
		description: "Show current working directory.",
	}
	commandMap["cd"] = Command{
		exec:        cdCommand,
		usage:       "cd <DIR>",
		description: "Change working directory to the specified directory",
	}
	commandMap["storage"] = Command{
		exec:  imageStorage,
		usage: "storage [list] or storage load [DIR]",
		description: "This command controls the images that are considered" +
			"database images. This does not mean that all these images have some" +
			"precomputed data, like histograms. Only that they were found as" +
			"possible images. You have to use other commands to load precomputed" +
			"data.\n\nIf \"list\" is used a list of all images will be printed" +
			"note that this can be quite large\n\n" +
			"if load is used the image storage will be initialized with images from" +
			"the directory (working directory if no image provided)",
	}
	commandMap["gch"] = Command{
		exec:  gchCommand,
		usage: "gch create [k] or gch TODO",
		description: "Used to administrate global color histograms (GCHs)\n\n" +
			"If \"create\" is used GCHs are created for all images in the current" +
			"storage. The optional argument k must be a number between 1 and 256." +
			"See usage documentation / Wiki for details about this value. 8 is the" +
			"default value and should be fine.",
	}
}

func getPath(state *ExecutorState, path string) (string, error) {
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

func helpCommand(state *ExecutorState, args ...string) bool {
	fmt.Println("The gomosaic generator runs in REPL mode, meaning you can" +
		"interactively generate mosaics by entering commands")
	fmt.Println("A list of commands follows")
	for _, cmd := range commandMap {
		fmt.Println()
		fmt.Println("Usage:", cmd.usage)
		fmt.Println(cmd.description)
	}
	return true
}

func exitCommand(state *ExecutorState, args ...string) bool {
	fmt.Println("Exiting gomosaic. Good bye!")
	os.Exit(0)
	// ereturn is requred
	return true
}

func pwdCommand(state *ExecutorState, args ...string) bool {
	fmt.Println(state.WorkingDir)
	return true
}

func cdCommand(state *ExecutorState, args ...string) bool {
	if len(args) != 1 {
		return false
	}
	path := args[0]
	var expandErr error
	path, expandErr = homedir.Expand(path)
	if expandErr != nil {
		fmt.Println("Error: Changing directory failed:", expandErr)
		return true
	}
	if fi, err := os.Lstat(path); err != nil {
		fmt.Println("Error: Changing directory failed:", err)
	} else {
		if fi.IsDir() {
			// convert to absolute path
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				fmt.Println("Error: Chaning directory failed:", absErr)
			} else {
				state.WorkingDir = abs
			}
		} else {
			fmt.Println("Error: Not a directory:", path)
		}
	}
	return true
}

func imageStorage(state *ExecutorState, args ...string) bool {
	switch {
	case len(args) == 0:
		fmt.Println("Number of database images:", state.Mapper.Len())
		return true
	case args[0] == "list":
		for _, path := range state.Mapper.IDMapping {
			fmt.Printf("  %s\n", path)
		}
		fmt.Println("Total:", state.Mapper.Len())
		return true
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
				return false
			}
			// parse path argument
			fallthrough
		case len(args) > 1:
			// parse the path
			var pathErr error
			dir, pathErr = getPath(state, args[1])
			if pathErr != nil {
				fmt.Println("Error: Can't get path:", pathErr)
				return true
			}
		default:
			// just to be sure, should never happen
			return false
		}
		fmt.Println("Loading images from", dir)
		if recursive {
			fmt.Println("Recursive mode enabled")
		}
		state.Mapper.Clear()
		if loadErr := state.Mapper.Load(dir, recursive, gomosaic.JPGAndPNG); loadErr != nil {
			fmt.Println("Error: Can't read image list:", loadErr)
			fmt.Println("Clearing the storage, including images previously added")
			state.Mapper.Clear()
		} else {
			fmt.Println("Successfully read", state.Mapper.Len(), "images")
			fmt.Println("Don't forget to (re)load precomputed data if required!")
		}
		return true
	default:
		return false
	}
}

func gchCommand(state *ExecutorState, args ...string) bool {
	switch {
	case len(args) == 0:
		return false
	case args[0] == "create":
		// k is the number of subdivions, defaults to 8
		var k uint = 8
		if len(args) > 1 {
			asInt, parseErr := strconv.Atoi(args[1])
			if parseErr != nil {
				return false
			}
			// validate k: must be >= 1 and <= 256
			if asInt < 1 || asInt > 256 {
				return false
			}
			k = uint(asInt)
		}

		// create all histograms
		fmt.Printf("Creating histograms for all images in storage with k = %d sub-divisions\n", k)
		var progress gomosaic.ProgressFunc
		if state.Verbose {
			progress = gomosaic.StdProgressFunc("", int(state.ImgStorage.NumImages()), 100)
		}
		histograms, histErr := gomosaic.CreateAllHistograms(state.ImgStorage,
			true, k, state.NumRoutines, progress)
		if histErr != nil {
			fmt.Println("Error: Can't create histograms:", histErr)
			return true
		}
		fmt.Printf("Computed %d histograms\n", len(histograms))
		return true
	default:
		return false
	}

}

func repl(state *ExecutorState) {
	fmt.Println("Welcome to the gomosaic generator")
	fmt.Println("Copyright © 2018 Fabian Wenzelmann")
	fmt.Println()
	fmt.Println("type \"help\" if you don't know what to do")
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print(">>> ")
	for scanner.Scan() {
		// we run an additional method to defer the print of >>>
		// otherwise becomes too ugly and we may miss it
		func() {
			defer func() {
				fmt.Print(">>> ")
			}()
			line := scanner.Text()
			parsedCmd, parseErr := parseCommand(line)
			if parseErr != nil {
				fmt.Println("Syntax error")
				return
			}
			if len(parsedCmd) == 0 {
				return
			}
			cmd := parsedCmd[0]
			if replCmd, ok := commandMap[cmd]; ok {
				if !replCmd.exec(state, parsedCmd[1:]...) {
					fmt.Println("Invalid command syntax")
					fmt.Println("Usage:", replCmd.usage)
				}
			} else {
				fmt.Printf("Unknown command \"%s\"\n", cmd)
			}
		}()
	}

	if scannErr := scanner.Err(); scannErr != nil {
		log.WithError(scannErr).Fatal("Error reading stdin")
	}
}
