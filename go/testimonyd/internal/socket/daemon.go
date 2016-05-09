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
	"io"
	"log"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"github.com/google/testimony/go/protocol"
	"github.com/google/testimony/go/testimonyd/internal/vlog"
)

// Testimony is the configuration parsed from the config file.
type Testimony []SocketConfig

const protocolVersion = 2

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
	connStr := c.RemoteAddr().String()
	log.Printf("Received new connection %q", connStr)
	var version [1]byte
	version[0] = protocolVersion
	if _, err := c.Write(version[:]); err != nil {
		log.Printf("new conn %q failed to write version: %v", connStr, err)
		return
	}
	conf := socks[0].conf
	if err := protocol.SendUint32(c, protocol.TypeFanoutSize, uint32(len(socks))); err != nil {
		log.Printf("new conn %q failed to send fanout size: %v", connStr, err)
		return
	}
	if err := protocol.SendUint32(c, protocol.TypeBlockSize, uint32(conf.BlockSize)); err != nil {
		log.Printf("new conn %q failed to send block size: %v", connStr, err)
		return
	}
	if err := protocol.SendUint32(c, protocol.TypeNumBlocks, uint32(conf.NumBlocks)); err != nil {
		log.Printf("new conn %q failed to send number of blocks: %v", connStr, err)
		return
	}
	if err := protocol.SendType(c, protocol.TypeWaitingForFanoutIndex); err != nil {
		log.Printf("new conn %q failed to send wait: %v", connStr, err)
		return
	}
	var fanoutMsg [8]byte
	if _, err := io.ReadFull(c, fanoutMsg[:]); err == io.EOF {
		log.Printf("new conn %q closed early, probably just gathering connection data", connStr)
		return
	} else if err != nil {
		log.Printf("new conn %q failed to read fanout index: %v", connStr, err)
		return
	}
	valA, valB := binary.BigEndian.Uint32(fanoutMsg[:4]), binary.BigEndian.Uint32(fanoutMsg[4:])
	if valA != protocol.ToTL(protocol.TypeFanoutIndex, 4) {
		log.Printf("new conn %q got unexpected type/value waiting for fanout message: %d/%d", connStr, valA>>16, valA&0xFFFF)
		return
	}
	idx := int(valB)
	if idx < 0 || idx >= len(socks) {
		log.Printf("new conn %q invalid index %v", connStr, idx)
		return
	}
	sock := socks[idx]
	fdMsg := syscall.UnixRights(sock.fd)
	var msg [1]byte // dummy byte
	n, n2, err := c.WriteMsgUnix(
		msg[:], fdMsg, nil)
	if err != nil || n != len(msg) || n2 != len(fdMsg) {
		log.Printf("new conn %q failed to send file descriptor: %v", connStr, err)
		return
	}
	vlog.V(2, "new conn %q spun up, passing off to socket", connStr)
	sock.newConns <- c
	c = nil // so it doesn't get closed by deferred func.
}
