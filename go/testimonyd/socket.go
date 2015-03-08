package main

// #include "daemon.h"
// #include <linux/if_packet.h>
// #include <stdlib.h>  // for C.free
import "C"

import (
	"fmt"
	"io"
	"log"
	"net"
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
	for i := 0; i < sc.NumBlocks; i++ {
		s.blocks[i] = &block{s: s, index: i}
	}
	iface := C.CString(sc.Interface)
	defer C.free(unsafe.Pointer(iface))
	var fd C.int
	var ring unsafe.Pointer
	if _, err := C.AFPacket(iface, C.int(sc.blockSize()), C.int(sc.NumBlocks),
		C.int(sc.BlockTimeoutMillis), C.int(fanoutID), C.int(sc.FanoutType),
		&fd, &ring); err != nil {
		return nil, err
	}
	s.fd = int(fd)
	s.ring = uintptr(ring)
	return s, nil
}

func (s *Socket) String() string {
	return fmt.Sprintf("[S:%v:%v]", s.conf.SocketName, s.num)
}

func (s *Socket) getNewBlocks() {
	for {
		b := s.blocks[s.index]
		for !b.ready() {
			time.Sleep(time.Millisecond)
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
			var buf [1]byte
			n, err := c.c.Read(buf[:])
			if err == io.EOF {
				return
			} else if err != nil || n != 1 {
				v(1, "%v read error (%d bytes): %v", c, n, err)
				return
			}
			i := int(buf[0])
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
				buf := [1]byte{byte(b.index)}
				if n, err := c.c.Write(buf[:]); err != nil || n != len(buf) {
					v(1, "%v write error for %v (%d bytes): %v", c, b, n, err)
					return
				}
			}
		}
	}()
	select {
	case <-readDone:
	case <-writeDone:
	}
	v(1, "%v closing", c)
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
	v(1, "%v new connection", s)
	newConn := &conn{
		s:         s,
		c:         c,
		newBlocks: make(chan *block),
	}
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
	blockDesc := (*C.struct_tpacket_block_desc)(unsafe.Pointer(b.s.ring + uintptr(b.s.conf.blockSize()*b.index)))
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
