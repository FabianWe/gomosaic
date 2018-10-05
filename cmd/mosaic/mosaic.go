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
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"strings"
	// Since we're not in the gomosaic package we have to import it
	"github.com/FabianWe/gomosaic"

	log "github.com/sirupsen/logrus"
)

func usage() {

}

func main() {
	if len(os.Args) == 1 {
		repl()
	}
	switch os.Args[1] {
	case "repl", "--repl":
		repl()
	case "exec", "execute", "--exec", "--execute":
		// read commands and execute them, assume separation by semicolon
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Error: exec requires a command to execute")
			usage()
			os.Exit(1)
		}
		// now join them by \n so that scanner reads them correctly
		cmds := strings.Replace(os.Args[2], ",", "\n", -1)
		r := strings.NewReader(cmds)
		script(r)
	case "script", "run", "--script", "--run":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Error: script requires a script to execute")
			usage()
			os.Exit(1)
		}
		// read file and execute
		f, err := os.Open(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: Can't open script", err)
		}
		defer f.Close()
		script(f)
	}
}

var cmdMap gomosaic.CommandMap

func init() {
	if gomosaic.Debug {
		log.SetLevel(log.DebugLevel)
	}
	// copy default commands, add additional methods
	cmdMap = make(gomosaic.CommandMap, 20)
	// This is a bit copy and paste, but DefaultCommands is also created in an
	// init function... to be sure everything works fine this will just
	// repeat the basic commands.
	// On the bright side: We can easier control what happens in the repl ;)
	cmdMap["pwd"] = gomosaic.Command{
		Exec:        gomosaic.PwdCommand,
		Usage:       "pwd",
		Description: "Show current working directory.",
	}
	cmdMap["cd"] = gomosaic.Command{
		Exec:        gomosaic.CdCommand,
		Usage:       "cd <DIR>",
		Description: "Change working directory to the specified directory",
	}
	cmdMap["storage"] = gomosaic.Command{
		Exec:  gomosaic.ImageStorageCommand,
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
	cmdMap["gch"] = gomosaic.Command{
		Exec:  gomosaic.GCHCommand,
		Usage: "gch create [k] or gch TODO",
		Description: "Used to administrate global color histograms (GCHs)\n\n" +
			"If \"create\" is used GCHs are created for all images in the current" +
			"storage. The optional argument k must be a number between 1 and 256." +
			"See usage documentation / Wiki for details about this value. 8 is the" +
			"default value and should be fine.",
	}

	cmdMap["mosaic"] = gomosaic.Command{
		Exec:        gomosaic.MosaicCommand,
		Usage:       "TODO",
		Description: "TODO",
	}

	// add exit command
	cmdMap["exit"] = gomosaic.Command{
		Exec:        exitCommand,
		Usage:       "exit",
		Description: "Close the program.",
	}

	// add help command
	cmdMap["help"] = gomosaic.Command{
		Exec:        helpCommand,
		Usage:       "help",
		Description: "Print this help message",
	}
}

func exitCommand(state *gomosaic.ExecutorState, args ...string) error {
	fmt.Println("Exiting the mosaic generator. Good bye!")
	os.Exit(0)
	return nil
}

func helpCommand(state *gomosaic.ExecutorState, args ...string) error {
	fmt.Println("The mosaic generator runs in REPL mode, meaning you can type" +
		"commands now to create a mosaic. See Wiki / website for details.")
	fmt.Println()
	fmt.Println("Commands")
	for _, cmd := range cmdMap {
		fmt.Println()
		fmt.Println("Usage:", cmd.Usage)
		fmt.Println(cmd.Description)
	}
	return nil
}

func repl() {
	// panics of Init in ReplHandler
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "Unable to initialize engine or some other error or bug! Exiting.")
			os.Exit(1)
		}
	}()
	gomosaic.Execute(gomosaic.ReplHandler{}, cmdMap)
}

func script(r io.Reader) {
	// panics of Init in ScriptHandler
	h := gomosaic.NewScriptHandler(r)
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Unable to initialize engine or some other error or bug! Exiting.")
			os.Exit(1)
		}
	}()
	gomosaic.Execute(h, cmdMap)
}
