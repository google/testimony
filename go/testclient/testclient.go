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

// testimony_testclient provides a simple client for testing block processing.
package main

import (
	"flag"
	"log"
	"time"

	"github.com/google/testimony/go/testimony"
)

var (
	socketName = flag.String("socket", "", "Name of testimony socket")
	fanoutInt  = flag.Int("fanout", 0, "Fanout number, if applicable")
)

func main() {
	flag.Parse()

	log.Printf("connecting to %q", *socketName)
	conn, err := testimony.Connect(*socketName)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	log.Printf("setting fanout to %d", *fanoutInt)
	if err := conn.Init(*fanoutInt); err != nil {
		log.Fatalf("failed to set fanout: %v", err)
	}

	log.Printf("reading blocks")
	totalCount := 0
	blockNum := 0
	start := time.Now()
	for {
		log.Printf("getting block")
		block, err := conn.Block()
		if err != nil {
			log.Fatalf("block reading failed: %v", err)
		}
		log.Printf("processing block")
		blockNum++
		blockCount := 0
		for block.Next() {
			blockCount++
		}
		log.Printf("returning block")
		if err := block.Return(); err != nil {
			log.Fatalf("block return failed: %v", err)
		}
		totalCount += blockCount
		log.Printf("block %d had %d packets, %d total in %v", blockNum, blockCount, totalCount, time.Since(start))
	}
}
