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
		repl(&gomosaic.ExecutorState{
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

var commandMap map[string]gomosaic.Command

func init() {
	if gomosaic.Debug {
		log.SetLevel(log.DebugLevel)
	}

	commandMap = make(map[string]gomosaic.Command, 20)
	commandMap["help"] = gomosaic.Command{
		Exec:        helpCommand,
		Usage:       "help",
		Description: "Show help.",
	}
	commandMap["exit"] = gomosaic.Command{
		Exec:        exitCommand,
		Usage:       "exit",
		Description: "Exit the program.",
	}
	commandMap["pwd"] = gomosaic.Command{
		Exec:        pwdCommand,
		Usage:       "pwd",
		Description: "Show current working directory.",
	}
	commandMap["cd"] = gomosaic.Command{
		Exec:        cdCommand,
		Usage:       "cd <DIR>",
		Description: "Change working directory to the specified directory",
	}
	commandMap["storage"] = gomosaic.Command{
		Exec:  imageStorage,
		Usage: "storage [list] or storage load [DIR]",
		Description: "This command controls the images that are considered" +
			"database images. This does not mean that all these images have some" +
			"precomputed data, like histograms. Only that they were found as" +
			"possible images. You have to use other commands to load precomputed" +
			"data.\n\nIf \"list\" is used a list of all images will be printed" +
			"note that this can be quite large\n\n" +
			"if load is used the image storage will be initialized with images from" +
			"the directory (working directory if no image provided)",
	}
	commandMap["gch"] = gomosaic.Command{
		Exec:  gchCommand,
		Usage: "gch create [k] or gch TODO",
		Description: "Used to administrate global color histograms (GCHs)\n\n" +
			"If \"create\" is used GCHs are created for all images in the current" +
			"storage. The optional argument k must be a number between 1 and 256." +
			"See usage documentation / Wiki for details about this value. 8 is the" +
			"default value and should be fine.",
	}
}

func helpCommand(state *gomosaic.ExecutorState, args ...string) bool {
	fmt.Println("The gomosaic generator runs in REPL mode, meaning you can" +
		"interactively generate mosaics by entering commands")
	fmt.Println("A list of commands follows")
	for _, cmd := range commandMap {
		fmt.Println()
		fmt.Println("Usage:", cmd.Usage)
		fmt.Println(cmd.Description)
	}
	return true
}

func exitCommand(state *gomosaic.ExecutorState, args ...string) bool {
	fmt.Println("Exiting gomosaic. Good bye!")
	os.Exit(0)
	// ereturn is requred
	return true
}

func pwdCommand(state *gomosaic.ExecutorState, args ...string) bool {
	fmt.Println(state.WorkingDir)
	return true
}

func cdCommand(state *gomosaic.ExecutorState, args ...string) bool {
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

func imageStorage(state *gomosaic.ExecutorState, args ...string) bool {
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
			dir, pathErr = state.GetPath(args[1])
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

func gchCommand(state *gomosaic.ExecutorState, args ...string) bool {
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

func repl(state *gomosaic.ExecutorState) {
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
			parsedCmd, parseErr := gomosaic.ParseCommand(line)
			if parseErr != nil {
				fmt.Println("Syntax error")
				return
			}
			if len(parsedCmd) == 0 {
				return
			}
			cmd := parsedCmd[0]
			if replCmd, ok := commandMap[cmd]; ok {
				if !replCmd.Exec(state, parsedCmd[1:]...) {
					fmt.Println("Invalid command syntax")
					fmt.Println("Usage:", replCmd.Usage)
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
