// Copyright 2015 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package vlog

import (
	"flag"
	"log"
	"path/filepath"
	"runtime"
)

var verbose = flag.Int("v", 0, "Verbose logging, increase for more logs")

// V logs a message based on the --v command line flag.
func V(level int, format string, args ...interface{}) {
	VUp(level, 1, format, args...)
}

// Vup logs a message based on the --v command line flag, using the n'th
// caller's file/line number instead of this one.
func VUp(level int, caller int, format string, args ...interface{}) {
	if level <= *verbose {
		_, file, line, _ := runtime.Caller(caller + 1)
		args = append([]interface{}{filepath.Base(file), line}, args...)
		log.Printf("%s:%d -\t"+format, args...)
	}
}
