package main

import (
	"flag"
	"os"

	"github.com/lxn/walk"
)

func main() {
	elevatedOp := flag.String("elevated-op", "", "internal: operation for the elevated helper")
	source := flag.String("source", "", "override source directory")
	target := flag.String("target", "", "override target directory")
	progressFile := flag.String("progress", "", "internal: progress file path")
	stateFile := flag.String("state", "", "override state file path")
	flag.Parse()

	if *elevatedOp != "" {
		os.Exit(runHeadless(*elevatedOp, *source, *target, *progressFile, *stateFile))
	}

	a := newApp()
	if *source != "" {
		a.source = *source
	}
	if *target != "" {
		a.target = *target
	}
	if *stateFile != "" {
		a.stateFile = *stateFile
	}
	if isDir(a.source) {
		a.srcState = srcPresent
	} else {
		a.srcState = srcMissing
	}

	if err := a.run(); err != nil {
		walk.MsgBox(nil, appTitle, err.Error(), walk.MsgBoxIconError)
		os.Exit(1)
	}
}
