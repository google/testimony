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

/*
#include <linux/if_packet.h>
#include <linux/filter.h>
#include <stdlib.h>  // for C.free

struct sock_fprog;

// See comments in socket.cc
int AFPacket(const char* iface, int block_size, int block_nr, int block_ms,
             int fanout_id, int fanout_size, int fanout_type,
             int filter_size, struct sock_filter* filters,
             // Outputs:
             int* fd, void** ring, const char** err);
*/
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
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/google/testimony/go/protocol"
	"github.com/google/testimony/go/testimonyd/internal/vlog"
)

// socket handles a single AF_PACKET socket.  There will be N Socket objects for
// each SocketConfig, where N == FanoutSize.  This Socket stores the file
// descriptor and memory region of a single underlying AF_PACKET socket.
type socket struct {
	num          int                // fanout index for this socket
	conf         SocketConfig       // configuration
	fd           int                // file descriptor for AF_PACKET socket
	newConns     chan *net.UnixConn // new client connections come in here
	oldConns     chan *conn         // old client connections come in here for cleanup
	newBlocks    chan *block        // when a new block is available, it comes in here
	blocks       []*block           // all blocks in the memory region
	currentConns map[*conn]bool     // list of current connections a new block will be sent to
	ring         uintptr            // pointer to memory region
}

// newSocket creates a new Socket object based on a config.
func newSocket(sc SocketConfig, fanoutID int, num int) (*socket, error) {
	s := &socket{
		num:          num,
		conf:         sc,
		newConns:     make(chan *net.UnixConn),
		oldConns:     make(chan *conn),
		newBlocks:    make(chan *block, sc.NumBlocks),
		currentConns: map[*conn]bool{},
		blocks:       make([]*block, sc.NumBlocks),
	}

	// Compile the BPF filter, if it was requested.
	var filt *C.struct_sock_filter
	var filtsize C.int
	if sc.Filter != "" {
		f, err := compileFilter(sc.Interface, sc.Filter)
		if err != nil {
			return nil, fmt.Errorf("unable to compile filter %q on interface %q: %v", sc.Filter, sc.Interface, err)
		}
		filt = &f[0]
		filtsize = C.int(len(f))
	}

	// Set up block objects, used to reference count blocks for clients.
	for i := 0; i < sc.NumBlocks; i++ {
		s.blocks[i] = &block{s: s, index: i}
	}

	// Call into our C code to actually create the socket.
	iface := C.CString(sc.Interface)
	defer C.free(unsafe.Pointer(iface))
	var fd C.int
	var ring unsafe.Pointer
	var errStr *C.char
	if _, err := C.AFPacket(iface, C.int(sc.BlockSize), C.int(sc.NumBlocks),
		C.int(sc.BlockTimeoutMillis), C.int(fanoutID), C.int(sc.FanoutSize), C.int(sc.FanoutType),
		filtsize, filt,
		&fd, &ring, &errStr); err != nil {
		return nil, fmt.Errorf("C AFPacket call failed: %v: %v", C.GoString(errStr), err)
	}
	s.fd = int(fd)
	s.ring = uintptr(ring)
	log.Printf("%v set up with %+v", s, sc)
	return s, nil
}

// String returns a unique string for this socket.
func (s *socket) String() string {
	return fmt.Sprintf("[S:%v:%v]", s.conf.SocketName, s.num)
}

// getNewBlocks is a goroutine that watches for new available packet blocks,
// which the run() method passes to clients.
func (s *socket) getNewBlocks() {
	blockIndex := 0
	sleep := time.Millisecond
	for {
		b := s.blocks[blockIndex]
		for !b.ready() {
			time.Sleep(sleep)
			if sleep < time.Second/4 {
				sleep *= 2
			}
		}
		b.ref()
		vlog.V(3, "%v got new block %v", s, b)
		s.newBlocks <- b
		blockIndex = (blockIndex + 1) % s.conf.NumBlocks
	}
}

// run handles new connections, old connections, new blocks... basically
// everything.
func (s *socket) run() {
	go s.getNewBlocks()
	for {
		select {
		case c := <-s.newConns:
			// register a new client connection
			s.addNewConn(c)
		case c := <-s.oldConns:
			// unregister an old client connection and close its blocks
			close(c.newBlocks)
			delete(s.currentConns, c)
		case b := <-s.newBlocks:
			// a new block is avaiable, send it out to all clients
			for c, _ := range s.currentConns {
				b.ref()
				select {
				case c.newBlocks <- b:
				default:
					vlog.V(1, "failed to send %v to %v", b, c)
					b.unref()
				}
			}
			b.unref()
		}
	}
}

// conn represents a set-up client connection (already initiated and with the
// file descriptor passed through).
type conn struct {
	s         *socket
	c         *net.UnixConn
	newBlocks chan *block
	oldBlocks chan int
}

// String returns a unique string for this connection.
func (c *conn) String() string {
	return fmt.Sprintf("[C:%v:%v]", c.s, c.c.RemoteAddr())
}

// handleReads handles client->server communication.
func (c *conn) handleReads() {
	defer close(c.oldBlocks)
	for {
		// Wait for a block index to be passed back from the client.
		var buf [4]byte
		n, err := c.c.Read(buf[:])
		if err == io.EOF {
			return
		} else if err != nil || n != len(buf) {
			vlog.V(1, "%v read error (%d bytes): %v", c, n, err)
			return
		}
		num := binary.BigEndian.Uint32(buf[:])
		if num&0x80000000 != 0 {
			typ, length := protocol.TLFrom(num)
			if protocol.TypeOf(typ) != protocol.TypeClientToServer {
				vlog.V(1, "%v client sent bad type %d", c, typ)
			}
			var val []byte
			if length != 0 {
				val = make([]byte, length)
				if _, err := io.ReadFull(c.c, val); err != nil {
					vlog.V(1, "%v read TLV %d length %d: %v", c, typ, length, err)
					return
				}
			}
			if err := c.handleTLV(typ, val); err != nil {
				vlog.V(1, "%v handling type %d: %v", c, typ, val)
				return
			}
		} else {
			i := int(num)
			if i < 0 || i >= c.s.conf.NumBlocks {
				log.Printf("%v got invalid block %d", c, i)
				return
			}
			// We add one to the returned int so we can detect a closed channel (which
			// returns 0, the zero-value for ints).
			c.oldBlocks <- i + 1
		}
	}
}

func (c *conn) handleTLV(typ protocol.Type, val []byte) error {
	log.Printf("IGNORING TLV: %d = %x", typ, val)
	return nil
}

// run handles communicating with a single external client via a single
// connection.  It maintains the invariant that every block it gets via the
// newBlocks channel will be unref'd exactly once.  It's up to the block sender
// to ref the blocks for the conn.
func (c *conn) run() {
	go c.handleReads()
	outstanding := make([]time.Time, len(c.s.blocks))

	// Wait for either the reader or writer to stop.
	var out []byte
loop:
	for {
		select {
		case b := <-c.newBlocks:
			out = out[:0]
			vlog.V(2, "%v writing %v", c, b)
		blockLoop:
			for {
				if !outstanding[b.index].IsZero() {
					log.Fatalf("%v received already outstanding block %v", c, b)
				}
				outstanding[b.index] = time.Now()
				idx := len(out)
				out = append(out, 0, 0, 0, 0)
				binary.BigEndian.PutUint32(out[idx:], uint32(b.index))
				select {
				case b = <-c.newBlocks:
					vlog.V(2, "%v batching %v", c, b)
				default:
					break blockLoop
				}
			}
			if _, err := c.c.Write(out); err != nil {
				vlog.V(1, "%v write error: %v", c, err)
				break loop
			}
		case i := <-c.oldBlocks:
			if i == 0 {
				// read loop is closed
				break loop
			}
			i-- // We added 1 to index in handleReads, remove 1 to get back to correct index.
			if outstanding[i].IsZero() {
				log.Printf("%v received non-outstanding block %v from client", c, i)
				break loop
			}
			b := c.s.blocks[i]
			vlog.V(3, "%v took %v to process block %v", c, time.Since(outstanding[i]), b)
			outstanding[i] = time.Time{}
			b.unref() // MOST IMPORTANT LINE EVER
		}
	}

	// Close things down.
	log.Printf("Connection %v closing", c)
	c.c.Close()
	vlog.V(3, "%v marking self old", c)
	c.s.oldConns <- c
	vlog.V(3, "%v waiting for reads", c)
	for b := range c.newBlocks {
		vlog.V(3, "%v returning unsent %v", c, b)
		b.unref()
	}
	// empty out oldBlocks to allow handleReads to finish, but don't do anything
	// with them.  the next loop (over outstanding) will unref and return all
	// remaining blocks.
	for _ = range c.oldBlocks {
	}
	for i, t := range outstanding {
		if !t.IsZero() {
			b := c.s.blocks[i]
			vlog.V(3, "%v returning outstanding %v after %v", c, b, time.Since(t))
			b.unref()
		}
	}
}

// addNewConn is called by the testimonyd server when a new connection has been
// initiated.  The passed-in conn should already have done the initial
// configuration handshake, and be ready to start receiving blocks.
func (s *socket) addNewConn(c *net.UnixConn) {
	newConn := &conn{
		s:         s,
		c:         c,
		newBlocks: make(chan *block, len(s.blocks)),
		oldBlocks: make(chan int, len(s.blocks)),
	}
	log.Printf("%v new connection %v", s, newConn)
	s.currentConns[newConn] = true
	go newConn.run()
}

// block stores ilocal information on a single block within the memory region.
type block struct {
	s     *socket
	index int // my index within the memory block

	r int32 // reference count for this block, uses atomic
}

// ref reference the block.
func (b *block) ref() {
	refs := atomic.AddInt32(&b.r, 1)
	vlog.VUp(5, 1, "%v refs = %d", b, refs)
}

// unref dereferences the block.  When the refcount reaches zero, the block is
// returned to the kernel via clear().
func (b *block) unref() {
	refs := atomic.AddInt32(&b.r, -1)
	vlog.VUp(5, 1, "%v unref = %d", b, refs)
	if refs == 0 {
		b.clear()
	} else if refs < 0 {
		panic(fmt.Sprintf("invalid unref of %v to %d", b, refs))
	}
}

// String provides a unique human-readable string.
func (b *block) String() string {
	return fmt.Sprintf("[B:%v:%v]", b.s, b.index)
}

// cblock provides this block as a C tpacket pointer.
func (b *block) cblock() *C.struct_tpacket_hdr_v1 {
	blockDesc := (*C.struct_tpacket_block_desc)(unsafe.Pointer(b.s.ring + uintptr(b.s.conf.BlockSize)*uintptr(b.index)))
	hdr := (*C.struct_tpacket_hdr_v1)(unsafe.Pointer(&blockDesc.hdr[0]))
	return hdr
}

// clear clears the block's block status, returning the block to the kernel so
// it can add additional packets.
func (b *block) clear() {
	vlog.VUp(3, 2, "%v clear", b)
	b.cblock().block_status = 0
}

// ready returns true when the block status has been set by the kernel, saying
// that packets are ready for processing.
func (b *block) ready() bool {
	return atomic.LoadInt32(&b.r) == 0 && b.cblock().block_status != 0
}

// compileFilter compiles a BPF filter, currently by calling tcpdump externally.
func compileFilter(iface, filt string) ([]C.struct_sock_filter, error) {
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
	ints = ints[1:]
	var bpfs []C.struct_sock_filter
	for i := 0; i < len(ints); i += 4 {
		bpfs = append(bpfs, C.struct_sock_filter{
			code: C.__u16(ints[i]),
			jt:   C.__u8(ints[i+1]),
			jf:   C.__u8(ints[i+2]),
			k:    C.__u32(ints[i+3]),
		})
	}
	return bpfs, nil
}
