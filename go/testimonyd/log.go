package main

import (
	"flag"
	"log"
	"path/filepath"
	"runtime"
)

var verbose = flag.Int("v", 0, "Verbose logging, increase for more logs")

func v(level int, format string, args ...interface{}) {
	vup(level, 1, format, args...)
}

func vup(level int, caller int, format string, args ...interface{}) {
	if level <= *verbose {
		_, file, line, _ := runtime.Caller(caller + 1)
		args = append([]interface{}{filepath.Base(file), line}, args...)
		log.Printf("%s:%d -\t"+format, args...)
	}
}
