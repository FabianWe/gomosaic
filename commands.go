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
	"errors"
	"io"
	"path/filepath"

	homedir "github.com/mitchellh/go-homedir"
)

type ExecutorState struct {
	WorkingDir     string
	Mapper         *FSMapper
	ImgStorage     *FSImageDB
	NumRoutines    int
	HistController *HistogramFSController
	Verbose        bool
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

type CommandFunc func(state *ExecutorState, args ...string) bool

type Command struct {
	Exec        CommandFunc
	Usage       string
	Description string
}

type CommandExecutor interface {
	Init(s *ExecutorState)
	Before(s *ExecutorState)
	OnSuccess(s *ExecutorState)
	OnFailure(s *ExecutorState)
	Out() io.Writer
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

var commandMap map[string]Command

func RegisterCommand(name string, cmd Command) {
	commandMap[name] = cmd
}

func init() {
	// commandMap = make(map[string]Command, 20)
	// commandMap["help"] = Command{
	// 	Exec:        helpCommand,
	// 	Usage:       "help",
	// 	Description: "Show help.",
	// }
	// commandMap["exit"] = Command{
	// 	Exec:        exitCommand,
	// 	Usage:       "exit",
	// 	Description: "Exit the program.",
	// }
	// commandMap["pwd"] = Command{
	// 	Exec:        pwdCommand,
	// 	Usage:       "pwd",
	// 	Description: "Show current working directory.",
	// }
	// commandMap["cd"] = Command{
	// 	Exec:        cdCommand,
	// 	Usage:       "cd <DIR>",
	// 	Description: "Change working directory to the specified directory",
	// }
	// commandMap["storage"] = Command{
	// 	Exec:  imageStorage,
	// 	Usage: "storage [list] or storage load [DIR]",
	// 	Description: "This command controls the images that are considered" +
	// 		"database images. This does not mean that all these images have some" +
	// 		"precomputed data, like histograms. Only that they were found as" +
	// 		"possible images. You have to use other commands to load precomputed" +
	// 		"data.\n\nIf \"list\" is used a list of all images will be printed" +
	// 		"note that this can be quite large\n\n" +
	// 		"if load is used the image storage will be initialized with images from" +
	// 		"the directory (working directory if no image provided)",
	// }
	// commandMap["gch"] = Command{
	// 	Exec:  gchCommand,
	// 	Usage: "gch create [k] or gch TODO",
	// 	Description: "Used to administrate global color histograms (GCHs)\n\n" +
	// 		"If \"create\" is used GCHs are created for all images in the current" +
	// 		"storage. The optional argument k must be a number between 1 and 256." +
	// 		"See usage documentation / Wiki for details about this value. 8 is the" +
	// 		"default value and should be fine.",
	// }
}
