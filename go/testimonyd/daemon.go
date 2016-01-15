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

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"log/syslog"
	"syscall"

	"github.com/google/testimony/go/testimonyd/internal/socket"
)

var (
	confFilename = flag.String("config", "/etc/testimony.conf", "Testimony config")
	logToSyslog  = flag.Bool("syslog", true, "log messages to syslog")
)

func main() {
	flag.Parse()
	if *logToSyslog {
		s, err := syslog.New(syslog.LOG_USER|syslog.LOG_INFO, "testimonyd")
		if err != nil {
			log.Fatalf("could not set up syslog logging: %v", err)
		}
		log.SetOutput(s)
	}
	log.Printf("Starting testimonyd...")
	confdata, err := ioutil.ReadFile(*confFilename)
	if err != nil {
		log.Fatalf("could not read configuration %q: %v", *confFilename, err)
	}
	// Set umask which will affect all of the sockets we create:
	syscall.Umask(0177)
	var t socket.Testimony
	if err := json.NewDecoder(bytes.NewBuffer(confdata)).Decode(&t); err != nil {
		log.Fatalf("could not parse configuration %q: %v", *confFilename, err)
	}
	socket.RunTestimony(t)
}
