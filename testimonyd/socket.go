package main

// #include "daemon.h"
// #include <linux/if_packet.h>
// #include <stdlib.h>  // for C.free
import "C"

import (
	"encoding/binary"
	"log"
	"net"
	"sync"
	"time"
	"unsafe"
)

type Socket struct {
	name         string
	num          int
	conf         SocketConfig
	fd           int
	newConns     chan *net.UnixConn
	oldConns     chan *conn
	newBlocks    chan int
	oldBlocks    chan int
	currentConns map[*conn]bool
	ring         uintptr
	blockUse     []int // protected by mu
	index        int
	mu           sync.Mutex
}

func newSocket(sc SocketConfig, fanoutID int, name string, num int) (*Socket, error) {
	s := &Socket{
		name:         name,
		num:          num,
		conf:         sc,
		newConns:     make(chan *net.UnixConn),
		oldConns:     make(chan *conn),
		newBlocks:    make(chan int),
		oldBlocks:    make(chan int),
		currentConns: map[*conn]bool{},
		blockUse:     make([]int, sc.NumBlocks),
	}
	iface := C.CString(sc.Interface)
	defer C.free(unsafe.Pointer(iface))
	var fd C.int
	var ring unsafe.Pointer
	if _, err := C.AFPacket(iface, C.int(sc.BlockSize), C.int(sc.NumBlocks),
		C.int(sc.BlockTimeoutMillis), C.int(fanoutID), C.int(sc.FanoutType),
		&fd, &ring); err != nil {
		return nil, err
	}
	s.ring = uintptr(ring)
	return s, nil
}

func (s *Socket) block(i int) *C.struct_tpacket_hdr_v1 {
	blockDesc := (*C.struct_tpacket_block_desc)(unsafe.Pointer(s.ring + uintptr(s.conf.BlockSize*i)))
	hdr := (*C.struct_tpacket_hdr_v1)(unsafe.Pointer(&blockDesc.hdr[0]))
	return hdr
}

func (s *Socket) blockReady(i int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blockUse[i] == 0 && s.block(i).block_status != 0
}

func (s *Socket) clearBlock(i int) {
	s.block(i).block_status = 0
}

func (s *Socket) getNewBlocks() {
	for {
		for !s.blockReady(s.index) {
			time.Sleep(time.Millisecond)
		}
		s.newBlocks <- s.index
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
		case b := <-s.oldBlocks:
			s.mu.Lock()
			s.blockUse[b]--
			v(2, "block %d in socket %q num %d down to %d uses", b, s.name, s.num, s.blockUse[b])
			if s.blockUse[b] == 0 {
				s.clearBlock(b)
			}
			s.mu.Unlock()
		case b := <-s.newBlocks:
			s.mu.Lock()
			if s.blockUse[b] != 0 {
				log.Fatalf("block %d in socket %q num %d already in use", b, s.name, s.num)
			}
			for c, _ := range s.currentConns {
				select {
				case c.newBlocks <- b:
					s.blockUse[b]++
				default:
					v(1, "connection failed to receive a block")
				}
			}
			blk := s.block(b)
			if s.blockUse[b] == 0 {
				v(2, "block %d in socket %q num %d with %d packets ignored", b, s.name, s.num, blk.num_pkts)
				s.clearBlock(b)
			} else {
				v(2, "block %d in socket %q num %d with %d packets sent to %d processes", b, s.name, s.num, blk.num_pkts, s.blockUse[b])
			}
			s.mu.Unlock()
		}
	}
}

type conn struct {
	s         *Socket
	c         *net.UnixConn
	newBlocks chan int
	oldBlocks chan int
}

func (c *conn) run() {
	outstanding := make([]bool, c.s.conf.NumBlocks)
	var mu sync.Mutex // protects outstanding
	readDone := make(chan struct{})
	writeDone := make(chan struct{})
	go func() { // handle reads
		defer close(readDone)
		for {
			var buf [4]byte
			n, err := c.c.Read(buf[:])
			if err != nil || n != 4 {
				v(1, "conn read error (%d bytes): %v", n, err)
				return
			}
			b := int(binary.BigEndian.Uint32(buf[:]))
			select {
			case <-writeDone:
				v(2, "conn read detected write closure")
				return
			case c.oldBlocks <- b:
				mu.Lock()
				outstanding[b] = false
				mu.Unlock()
			}
		}
	}()
	go func() {
		defer close(writeDone)
		for {
			select {
			case <-readDone:
				v(2, "conn write detected read closure")
				return
			case b := <-c.newBlocks:
				mu.Lock()
				outstanding[b] = true
				mu.Unlock()
				var buf [4]byte
				binary.BigEndian.PutUint32(buf[:], uint32(b))
				if n, err := c.c.Write(buf[:]); err != nil || n != 4 {
					v(1, "conn write error (%d bytes): %v", n, err)
					return
				}
			}
		}
	}()
	select {
	case <-readDone:
	case <-writeDone:
	}
	v(1, "closing conn")
	c.c.Close()
	<-readDone
	<-writeDone
	for b := range c.newBlocks {
		c.oldBlocks <- b
	}
	for b, got := range outstanding {
		if got {
			c.oldBlocks <- b
		}
	}
}

func (s *Socket) addNewConn(c *net.UnixConn) {
	v(1, "new connection on socket %q", s.name)
	newConn := &conn{
		s:         s,
		c:         c,
		newBlocks: make(chan int, 10),
		oldBlocks: make(chan int, 10),
	}
	s.currentConns[newConn] = true
	go newConn.run()
}
