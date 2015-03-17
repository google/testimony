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

// #include "daemon.h"
// #include <linux/if_packet.h>
// #include <linux/filter.h>
// #include <stdlib.h>  // for C.free
import "C"

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

type Socket struct {
	num          int
	conf         SocketConfig
	fd           int
	newConns     chan *net.UnixConn
	oldConns     chan *conn
	newBlocks    chan *block
	blocks       []*block
	currentConns map[*conn]bool
	ring         uintptr
	index        int
}

func newSocket(sc SocketConfig, fanoutID int, num int) (*Socket, error) {
	s := &Socket{
		num:          num,
		conf:         sc,
		newConns:     make(chan *net.UnixConn),
		oldConns:     make(chan *conn),
		newBlocks:    make(chan *block),
		currentConns: map[*conn]bool{},
		blocks:       make([]*block, sc.NumBlocks),
	}
	var filt *C.struct_sock_fprog
	if sc.Filter != "" {
		f, err := compileFilter(sc.Interface, sc.Filter)
		if err != nil {
			return nil, fmt.Errorf("unable to compile filter %q on interface %q: %v", sc.Filter, sc.Interface, err)
		}
		filt = &f.filt
	}
	for i := 0; i < sc.NumBlocks; i++ {
		s.blocks[i] = &block{s: s, index: i}
	}
	iface := C.CString(sc.Interface)
	defer C.free(unsafe.Pointer(iface))
	var fd C.int
	var ring unsafe.Pointer
	var errStr *C.char
	if _, err := C.AFPacket(iface, C.int(sc.BlockSize), C.int(sc.NumBlocks),
		C.int(sc.BlockTimeoutMillis), C.int(fanoutID), C.int(sc.FanoutType), filt,
		&fd, &ring, &errStr); err != nil {
		return nil, fmt.Errorf("C AFPacket call failed: %v: %v", C.GoString(errStr), err)
	}
	s.fd = int(fd)
	s.ring = uintptr(ring)
	log.Printf("%v set up with %+v", s, sc)
	return s, nil
}

func (s *Socket) String() string {
	return fmt.Sprintf("[S:%v:%v]", s.conf.SocketName, s.num)
}

func (s *Socket) getNewBlocks() {
	for {
		b := s.blocks[s.index]
		for !b.ready() {
			time.Sleep(time.Millisecond * 10)
		}
		b.ref()
		v(3, "%v got new block %v", s, b)
		s.newBlocks <- b
		s.index = (s.index + 1) % s.conf.NumBlocks
	}
}

func (s *Socket) run() {
	go s.getNewBlocks()
	for {
		select {
		case c := <-s.newConns:
			s.addNewConn(c)
		case c := <-s.oldConns:
			close(c.newBlocks)
			delete(s.currentConns, c)
		case b := <-s.newBlocks:
			for c, _ := range s.currentConns {
				b.ref()
				select {
				case c.newBlocks <- b:
				default:
					v(1, "failed to send %v to %v", b, c)
					b.unref()
				}
			}
			b.unref()
		}
	}
}

type conn struct {
	s         *Socket
	c         *net.UnixConn
	newBlocks chan *block
}

func (c *conn) String() string {
	return fmt.Sprintf("[C:%v:%v]", c.s, c.c.RemoteAddr())
}

// run handles communicating with a single external client via a single
// connection.  It maintains the invariant that every block it gets via the
// newBlocks channel will be unref'd exactly once.
func (c *conn) run() {
	outstanding := make([]time.Time, c.s.conf.NumBlocks)
	var mu sync.Mutex // protects outstanding
	readDone := make(chan struct{})
	writeDone := make(chan struct{})
	go func() { // handle reads
		defer close(readDone)
		for {
			var buf [4]byte
			n, err := c.c.Read(buf[:])
			if err == io.EOF {
				return
			} else if err != nil || n != len(buf) {
				v(1, "%v read error (%d bytes): %v", c, n, err)
				return
			}
			i := int(binary.BigEndian.Uint32(buf[:]))
			if i < 0 || i >= c.s.conf.NumBlocks {
				log.Printf("%v got invalid block %d", c, i)
				return
			}
			b := c.s.blocks[i]
			mu.Lock()
			t := outstanding[i]
			outstanding[i] = time.Time{}
			mu.Unlock()
			if t.IsZero() {
				log.Printf("%v returned %v that was not outstanding", c, b)
				return
			}
			v(4, "%v returned %v after %v", c, b, time.Since(t))
			b.unref()
			select {
			case <-writeDone:
				v(2, "%v read detected write closure", c)
				return
			default:
			}
		}
	}()
	go func() {
		defer close(writeDone)
		for {
			select {
			case <-readDone:
				v(2, "%v write detected read closure", c)
				return
			case b := <-c.newBlocks:
				mu.Lock()
				v(4, "%v sent %v to %v", c.s, b, c)
				outstanding[b.index] = time.Now()
				mu.Unlock()
				var buf [4]byte
				binary.BigEndian.PutUint32(buf[:], uint32(b.index))
				if _, err := c.c.Write(buf[:]); err != nil {
					v(1, "%v write error for %v: %v", c, b, err)
					return
				}
			}
		}
	}()
	select {
	case <-readDone:
	case <-writeDone:
	}
	log.Println("Connection %v closing", c)
	c.c.Close()
	v(3, "%v marking self old", c)
	c.s.oldConns <- c
	v(3, "%v waiting for reads", c)
	<-readDone
	v(3, "%v waiting for writes", c)
	<-writeDone
	v(3, "%v returning unsent blocks", c)
	for b := range c.newBlocks {
		v(3, "%v returning unsent %v", c, b)
		b.unref()
	}
	v(3, "%v returning outstanding blocks", c)
	for b, got := range outstanding {
		if !got.IsZero() {
			v(4, "%v returning outstanding %v after %v", c, b, time.Since(got))
			c.s.blocks[b].unref()
		}
	}
}

func (s *Socket) addNewConn(c *net.UnixConn) {
	newConn := &conn{
		s:         s,
		c:         c,
		newBlocks: make(chan *block),
	}
	log.Printf("%v new connection %v", s, newConn)
	s.currentConns[newConn] = true
	go newConn.run()
}

type block struct {
	s     *Socket
	mu    sync.Mutex
	r     int
	index int
}

func (b *block) ref() {
	b.mu.Lock()
	vup(5, 1, "%v ref %d->%d", b, b.r, b.r+1)
	b.r++
	b.mu.Unlock()
}

func (b *block) unref() {
	b.mu.Lock()
	vup(5, 1, "%v unref %d->%d", b, b.r, b.r-1)
	b.r--
	if b.r == 0 {
		b.clear()
	} else if b.r < 0 {
		panic(fmt.Sprintf("invalid unref of %v to %d", b, b.r))
	}
	b.mu.Unlock()
}

func (b *block) String() string {
	return fmt.Sprintf("[B:%v:%v]", b.s, b.index)
}

func (b *block) cblock() *C.struct_tpacket_hdr_v1 {
	blockDesc := (*C.struct_tpacket_block_desc)(unsafe.Pointer(b.s.ring + uintptr(b.s.conf.BlockSize)*uintptr(b.index)))
	hdr := (*C.struct_tpacket_hdr_v1)(unsafe.Pointer(&blockDesc.hdr[0]))
	return hdr
}

func (b *block) clear() {
	vup(3, 2, "%v clear", b)
	b.cblock().block_status = 0
}

func (b *block) ready() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.r == 0 && b.cblock().block_status != 0
}

type filter struct {
	bpfs []C.struct_sock_filter
	filt C.struct_sock_fprog
}

func compileFilter(iface, filt string) (*filter, error) {
	cmd := exec.Command("/usr/sbin/tcpdump", "-i", iface, "-ddd", filt)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not run tcpdump to compile BPF: %v", err)
	}
	ints := []int{}
	scanner := bufio.NewScanner(&out)
	scanner.Split(bufio.ScanWords)
	for scanner.Scan() {
		i, err := strconv.Atoi(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("error scanning token %q: %v", scanner.Text(), err)
		}
		ints = append(ints, i)
	}
	if len(ints) == 0 || len(ints) != ints[0]*4+1 {
		return nil, fmt.Errorf("invalid length of tcpdump ints")
	}
	f := &filter{}
	ints = ints[1:]
	for i := 0; i < len(ints); i += 4 {
		f.bpfs = append(f.bpfs, C.struct_sock_filter{
			code: C.__u16(ints[i]),
			jt:   C.__u8(ints[i+1]),
			jf:   C.__u8(ints[i+2]),
			k:    C.__u32(ints[i+3]),
		})
	}
	f.filt.len = C.ushort(len(f.bpfs))
	f.filt.filter = (*C.struct_sock_filter)(unsafe.Pointer(&f.bpfs[0]))
	return f, nil
}
