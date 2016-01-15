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

package socket

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"

  "github.com/google/testimony/go/testimonyd/internal/vlog"
)

// Testimony is the configuration parsed from the config file.
type Testimony []SocketConfig

const protocolVersion = 1

// SocketConfig defines how an individual socket should be set up.
type SocketConfig struct {
	SocketName         string // filename for the socket
	Interface          string // interface to sniff packets on
	BlockSize          int    // block size (in bytes) of a single packet block
	NumBlocks          int    // number of packet blocks in the memory region
	BlockTimeoutMillis int    // timeout for filling up a single block
	FanoutType         int    // which type of fanout to use (see linux/if_packet.h)
	FanoutSize         int    // number of threads to fan out to
	User               string // user to provide the socket to (will chown it)
	Filter             string // BPF filter to apply to this socket
}

func (s SocketConfig) uid() (int, error) {
	var u *user.User
	var err error
	if s.User == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(s.User)
	}
	if err != nil {
		return 0, fmt.Errorf("could not get user: %v", err)
	}
	return strconv.Atoi(u.Uid)
}

// RunTestimony runs the testimonyd server given the passed in configuration.
func RunTestimony(t Testimony) {
	fanoutID := 0
	names := map[string]bool{}
	for _, sc := range t {
		// Check for duplicate socket names
		if names[sc.SocketName] {
			log.Fatalf("invalid config: duplicate socket name %q", sc.SocketName)
		}
		names[sc.SocketName] = true

		// Set up FanoutSize sockets and start goroutines to manage each.
		var socks []*socket
		fanoutID++
		for i := 0; i < sc.FanoutSize; i++ {
			sock, err := newSocket(sc, fanoutID, i)
			if err != nil {
				log.Fatalf("invalid config %+v: %v", sc, err)
			}
			socks = append(socks, sock)
			go sock.run()
		}

		// Set up UNIX socket to serve these AF_PACKET sockets on, and start
		// goroutine to manage its connections.
		_ignore_error_ := os.Remove(sc.SocketName)
		_ = _ignore_error_
		list, err := net.ListenUnix("unix", &net.UnixAddr{Net: "unix", Name: sc.SocketName})
		if err != nil {
			log.Fatalf("failed to listen on socket: %v", err)
		} else if err := setPermissions(sc); err != nil {
			log.Fatalf("failed to set socket permissions: %v", err)
		}
		go t.run(list, sc, socks)
	}
	// We'd love to drop privs here, but thanks to
	// https://github.com/golang/go/issues/1435 we can't :(
	select {} // Block (serving) forever.
}

func setPermissions(sc SocketConfig) error {
	uid, err := sc.uid()
	if err != nil {
		return fmt.Errorf("could not get uid to change to: %v", err)
	}
	vlog.V(1, "chowning %q to %d", sc.SocketName, uid)
	if err := syscall.Chown(sc.SocketName, uid, 0); err != nil {
		return fmt.Errorf("unable to chown to (%d, 0): %v", uid, err)
	}
	return nil
}

func (t Testimony) run(list *net.UnixListener, sc SocketConfig, socks []*socket) {
	for {
		c, err := list.AcceptUnix()
		if err != nil {
			log.Fatalf("failed to accept connection: %v", err)
		}
		go t.handle(socks, c)
	}
}

func (t Testimony) handle(socks []*socket, c *net.UnixConn) {
	defer func() {
		if c != nil {
			c.Close()
		}
	}()
	log.Printf("Received new connection %v", c.RemoteAddr())
	var buf [13]byte
	buf[0] = protocolVersion
	binary.BigEndian.PutUint32(buf[1:], uint32(len(socks)))
	binary.BigEndian.PutUint32(buf[5:], uint32(socks[0].conf.BlockSize))
	binary.BigEndian.PutUint32(buf[9:], uint32(socks[0].conf.NumBlocks))
	if _, err := c.Write(buf[:]); err != nil {
		log.Printf("new conn failed to write version: %v", err)
		return
	}
	var fanoutMsg [4]byte
	if n, err := c.Read(fanoutMsg[:]); n != len(fanoutMsg) || err != nil {
		log.Printf("new conn failed to read conf: %v", err)
		return
	}
	idx := int(binary.BigEndian.Uint32(fanoutMsg[:]))
	if idx < 0 || idx >= len(socks) {
		log.Printf("new conn invalid index %v", idx)
		return
	}
	sock := socks[idx]
	fdMsg := syscall.UnixRights(sock.fd)
	var msg [1]byte // dummy byte
	n, n2, err := c.WriteMsgUnix(
		msg[:], fdMsg, nil)
	if err != nil || n != len(msg) || n2 != len(fdMsg) {
		log.Printf("new conn failed to send file descriptor: %v", err)
		return
	}
	vlog.V(2, "new conn spun up, passing off to socket")
	sock.newConns <- c
	c = nil // so it doesn't get closed by deferred func.
}
