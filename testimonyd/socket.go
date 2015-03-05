package main

// #include "daemon.h"
// #include <linux/if_packet.h>
// #include <stdlib.h>  // for C.free
import "C"

import (
	"encoding/binary"
	"log"
	"net"
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
	blockUse     []int
	index        int
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
			delete(s.currentConns, c)
		case b := <-s.oldBlocks:
			s.blockUse[b]--
			if s.blockUse[b] == 0 {
				s.clearBlock(b)
			}
		case b := <-s.newBlocks:
			if s.blockUse[b] != 0 {
				log.Fatalf("block %d in socket %q num %d already in use", b, s.name, s.num)
			}
			for c, _ := range s.currentConns {
				select {
				case c.newBlocks <- b:
					s.blockUse[b]++
				default:
					log.Printf("connection failed to receive a block")
				}
			}
			if s.blockUse[b] == 0 {
				s.clearBlock(b)
			}
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
	readDone := make(chan struct{})
	writeDone := make(chan struct{})
	allDone := false
	go func() { // handle reads
		defer close(readDone)
		for !allDone {
			var buf [4]byte
			n, err := c.c.Read(buf[:])
			if err != nil || n != 4 {
				log.Printf("conn read error (%d bytes): %v", n, err)
				return
			}
			c.oldBlocks <- int(binary.BigEndian.Uint32(buf[:]))
		}
	}()
	go func() {
		defer close(writeDone)
		for b := range c.newBlocks {
			if allDone {
				return
			}
			var buf [4]byte
			binary.BigEndian.PutUint32(buf[:], uint32(b))
			if n, err := c.c.Write(buf[:]); err != nil || n != 4 {
				log.Printf("conn write error (%d bytes): %v", n, err)
				return
			}
		}
	}()
	select {
	case <-readDone:
	case <-writeDone:
	}
	allDone = true
	log.Printf("closing conn")
	c.c.Close()
}

func (s *Socket) addNewConn(c *net.UnixConn) {
	log.Printf("new connection on socket %q", s.name)
	newConn := &conn{
		s:         s,
		c:         c,
		newBlocks: make(chan int, 10),
		oldBlocks: make(chan int, 10),
	}
	s.currentConns[newConn] = true
	go newConn.run()
}
