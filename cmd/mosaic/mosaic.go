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
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
	// Since we're not in the gomosaic package we have to import it
	"github.com/FabianWe/gomosaic"

	log "github.com/sirupsen/logrus"
)

func usage() {
	prefix := "Usage " + os.Args[0]
	prefixLength := utf8.RuneCountInString(prefix)
	prefixReplace := strings.Repeat(" ", prefixLength)
	fmt.Println(prefix, "[--version | -v] [--help | -h] [--copyright] [--repl] [--run <path> [params...]]")
	fmt.Println(prefixReplace, "[--execute <command> [params...]]")
	fmt.Println(prefixReplace, "[simple <db-path> <input> <output> <tilesX x tilesY> [width x height]]")
	fmt.Println(prefixReplace, "[metric <db-path> <input> <output> <tilesX x tilesY> <metric>]")
	fmt.Println(prefixReplace, "[compare <db-path> <input> <output-dir> <tilesX x tilesY>]")
	fmt.Println()
	fmt.Println("The commands mean the following:")
	fmt.Println()
	indent := strings.Repeat(" ", 2)
	type cmdDesc struct {
		cmd         string
		description []string
	}
	descriptions := []cmdDesc{
		cmdDesc{"--help", []string{"Show this message and exit"}},
		cmdDesc{"--version", []string{"Show version and exit"}},
		cmdDesc{"--copyright", []string{"Show copyright information and exit"}},
		cmdDesc{"--repl", []string{"Run interactive mode (Read–Eval–Print Loop)"}},
		cmdDesc{"--run", []string{
			"Run commands in the specified mosaic script file. Additional arguments",
			"are used for variable replacements.",
		}},
		cmdDesc{
			"--execute", []string{
				"Execute commands specified as the command argument. Commands must",
				"be separated by \";\".Additional arguments are used for variable",
				"replacements.",
			}},
		cmdDesc{
			"simple", []string{
				"Create a mosaic from images in the directory db-path. The image is",
				"created from image input and the mosaic written to output. tilesX",
				"and tilesY specify the number of tiles in the mosaic image. The",
				"optional width and height specify the size of the output, if omitted",
				"the mosaic has the same width and height as the input. You can also",
				"specify only width or height and keep the ratio of the input image.",
				"For example 1024x or x768.",
				"Example: simple ~/Pictures/ input.jpg output.png 20x30 1024x",
			}},
		cmdDesc{
			"metric", []string{
				"The same as --simple but with an additional metric argument. All",
				"arguments are required, to keep the input images dimensions simply",
				"use \"x\" for the dimension. Valid metrics listed below.",
				"Example: metric ~/Pictures/ input.jpg output.png 20x30 x cosine",
			}},
		cmdDesc{
			"compare", []string{
				"The same as --simple but output is specified by a directory in which",
				"several images are saved, computed with different metrics.",
				"Example: compare ~/Pictures/ input.jpg ./output/ 20x30 x768",
			}},
	}

	for _, desc := range descriptions {
		fmt.Printf("%s%s %s\n", indent, desc.cmd, desc.description[0])
		innerIndent := strings.Repeat(" ", 2+len(desc.cmd))
		// space included automatically by println later
		for _, line := range desc.description[1:] {
			fmt.Println(innerIndent, line)
		}
	}
	fmt.Println()
	fmt.Println("Available metrics:")
	fmt.Println(strings.Join(gomosaic.GetHistogramMetricNames(), " "))
}

func main() {
	if gomosaic.Debug {
		fmt.Println("gomosaic is running in debug mode")
	}
	if len(os.Args) == 1 {
		repl()
	}
	switch os.Args[1] {
	case "--help", "-h":
		usage()
	case "--version", "-v":
		fmt.Println("gomsaic version", gomosaic.Version)
	case "--copyright":
		// hack, but fine
		copyrightCommand(nil)
	case "--repl":
		repl()
	case "--execute":
		// read commands and execute them, assume separation by semicolon
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Error: exec requires a sequence of commands to execute")
			os.Exit(1)
		}
		// now join them by \n so that scanner reads them correctly
		cmds := strings.Replace(os.Args[2], ",", "\n", -1)
		r := strings.NewReader(cmds)
		script(r, os.Args[3:]...)
	case "--script", "--run":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Error: script requires a script file to execute")
			os.Exit(1)
		}
		// read file and execute
		f, err := os.Open(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: Can't open script", err)
			os.Exit(1)
		}
		defer f.Close()
		script(f, os.Args[3:]...)
	case "simple":
		simple(os.Args[2:])
	case "metric":
		metric(os.Args[2:])
	case "compare":
		compare(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Invalid command \"%s\"\n", os.Args[1])
		os.Exit(1)
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
	cmdMap["stats"] = gomosaic.Command{
		Exec:        gomosaic.StatsCommand,
		Usage:       "stats [var]",
		Description: "Show value of variables that can be changed via set, if var is given only value of that variable",
	}
	cmdMap["set"] = gomosaic.Command{
		Exec:  gomosaic.SetVarCommand,
		Usage: "set <variable> <value>",
		Description: "Set value for a variable. For details about the variables" +
			" please refer to the user documentation. To see all variables use \"stats\"",
	}
	cmdMap["cd"] = gomosaic.Command{
		Exec:        gomosaic.CdCommand,
		Usage:       "cd <dir>",
		Description: "Change working directory to the specified directory",
	}
	cmdMap["storage"] = gomosaic.Command{
		Exec:  gomosaic.ImageStorageCommand,
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
	cmdMap["gch"] = gomosaic.Command{
		Exec:  gomosaic.GCHCommand,
		Usage: "gch create [k] or gch load <file> or gch save <file>",
		Description: "Used to administrate global color histograms (GCHs)\n\n" +
			"If \"create\" is used GCHs are created for all images in the current" +
			" storage. The optional argument k must be a number between 1 and 256." +
			" See usage documentation / Wiki for details about this value. 8 is the" +
			" default value and should be fine.\n\nsave and load commands load files" +
			" containing GHCs from a file.",
	}
	cmdMap["lch"] = gomosaic.Command{
		Exec:  gomosaic.LCHCommand,
		Usage: "lch create <k> <scheme> or lch load <file> or lch save <file>",
		Description: "Used to administrate local color histograms (LCHs)\n\n" +
			"\"crate\", \"load\" and \"save\" work as in the gch command. k is also" +
			"the same as in the GCH command and scheme is the number of GCHs created" +
			"for each image (must be either 4 or 5).",
	}
	cmdMap["mosaic"] = gomosaic.Command{
		Exec:  gomosaic.MosaicCommand,
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
			"Example Usage: \"mosaic in.jpg out.jpg gch-cosine 20x30 1024x768\". Valid " +
			" metrics (each with prefix \"gch-\" like \"gch-cosine\"):\n\n" +
			strings.Join(gomosaic.GetHistogramMetricNames(), " "),
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

	// add copyright command
	cmdMap["copyright"] = gomosaic.Command{
		Exec:        copyrightCommand,
		Usage:       "copyright",
		Description: "Show copyright information",
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
	// keep order deterministic and sorted
	keys := make([]string, 0, len(cmdMap))
	for cmd := range cmdMap {
		keys = append(keys, cmd)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cmd := cmdMap[key]
		fmt.Println()
		fmt.Println("  Usage:", cmd.Usage)
		// split, looks nicer
		split := strings.Split(cmd.Description, "\n")
		for _, line := range split {
			fmt.Printf("    %s\n", line)
		}
	}
	return nil
}

func copyrightCommand(state *gomosaic.ExecutorState, args ...string) error {
	license := `  Copyright 2018 Fabian Wenzelmann

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.

  For more details and third-party licenses see <https://github.com/FabianWe/gomosaic>`
	fmt.Println(license)
	return nil
}

func repl() {
	// panics of Init in ReplHandler and all other panics
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "Unable to initialize engine or some other error or bug! Exiting.")
			fmt.Fprintln(os.Stderr, r)
			os.Exit(1)
		}
	}()
	gomosaic.Execute(gomosaic.ReplHandler{}, cmdMap)
}

func fromTemplate(template string, args ...string) {
	r := strings.NewReader(template)
	script(r, args...)
}

func script(r io.Reader, args ...string) {
	// panics of Init in ScriptHandler and all other panics
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "Unable to initialize engine or some other error or bug! Exiting.")
			os.Exit(1)
		}
	}()
	fmt.Println(args)
	//  check if args are given
	if len(args) > 0 {
		// parameterize lines
		var readErr error
		r, readErr = gomosaic.Parameterized(r, args...)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, "Error: Can't read script", readErr)
			os.Exit(1)
		}
	}
	h := gomosaic.NewScriptHandler(r)

	gomosaic.Execute(h, cmdMap)
}

func simple(args []string) {
	// ~/Pictures/ input.jpg output.png 20x30 1024x
	switch len(args) {
	case 4:
		args = append(args, "x")
	case 5:
		// do nothing
	default:
		fmt.Fprintln(os.Stderr, "Invalid syntax for --simple, requires 4 or 5 arguments, got", len(args))
		os.Exit(1)
	}
	fromTemplate(gomosaic.RunSimple, args...)
}

func metric(args []string) {
	if len(args) != 6 {
		fmt.Fprintln(os.Stderr, "Invalid syntax for --metric, requires exactly 6 arguments, got", len(args))
		os.Exit(1)
	}
	fromTemplate(gomosaic.RunMetric, args...)
}

func compare(args []string) {
	switch len(args) {
	case 4:
		args = append(args, "x")
	case 5:
		// do nothing
	default:
		fmt.Fprintln(os.Stderr, "Invalid syntax for --compare, requires 4 or 5 arguments, got", len(args))
		os.Exit(1)
	}
	// this is a rather ugly fix for windows
	cmd := filepath.FromSlash(gomosaic.CompareMetrics)
	fromTemplate(cmd, args...)
}
