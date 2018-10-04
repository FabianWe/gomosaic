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
	"runtime"
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
	// seems reasonable
	initialRoutines := runtime.NumCPU() * 2
	if initialRoutines <= 0 {
		// don't know if this can happen, better safe then sorry
		initialRoutines = 4
	}
	if len(os.Args) == 1 {
		repl(&replState{
			workingDir:  ".",
			numRoutines: initialRoutines,
		})
	}
}

type replState struct {
	workingDir  string
	numRoutines int
}

type replCommand func(state *replState, args ...string)

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

var commandMap map[string]replCommand

func init() {
	commandMap = make(map[string]replCommand, 20)
	commandMap["help"] = helpCommand
	commandMap["exit"] = exitCommand
}

func helpCommand(state *replState, args ...string) {
	fmt.Println("The gomosaic generator runs in REPL mode, meaning you can" +
		"interactively generate mosaics by entering commands")
	fmt.Println("A list of commands follows")
	fmt.Println()
	fmt.Println("help - Show this help text")
}

func exitCommand(state *replState, args ...string) {
	fmt.Println("Exiting gomosaic. Good bye!")
	os.Exit(0)
}

func repl(state *replState) {
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
			if replFunc, ok := commandMap[cmd]; ok {
				replFunc(state, parsedCmd[1:]...)
			} else {
				fmt.Printf("Unknown command\"%s\"\n", cmd)
			}
		}()
	}

	if scannErr := scanner.Err(); scannErr != nil {
		log.WithError(scannErr).Fatal("Error reading stdin")
	}
}
