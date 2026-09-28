package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chester-hill-solutions/stow-s3/internal/version"
)

// versionLine is the answer every spelling of the version flag gives. It reads
// internal/version, the single source the readiness payload and the doctor report
// already publish.
const versionLine = "stow-s3 " + version.Version

// versionFlag declares --version and returns whether it was given. Every
// subcommand registers it here: a flag added to three of four commands reads as a
// deliberate difference until someone runs the fourth.
func versionFlag(flags *flag.FlagSet) *bool {
	show := flags.Bool("version", false, "Print the stow-s3 version and exit")
	// A spelling rather than a second flag: the same variable.
	flags.BoolVar(show, "v", false, "Print the stow-s3 version and exit (shorthand for --version)")
	return show
}

// printVersion reports whether the caller should stop. Exit 0 is part of the
// contract: a version flag that fails is a bug report about the bug report tool.
func printVersion(out io.Writer, requested bool) bool {
	if !requested {
		return false
	}
	fmt.Fprintln(out, versionLine)
	return true
}

// versionRequested recognises the spellings that name no subcommand. `version` is
// accepted as a word because it is what a person types first.
func versionRequested(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "--version", "-version", "-v", "version":
		return true
	}
	return false
}

func topLevelVersion(args []string) bool {
	if !versionRequested(args) {
		return false
	}
	printVersion(os.Stdout, true)
	return true
}
