package main

// #include "daemon.h"
// #include <stdlib.h>  // for C.free
import "C"

import (
	"log"
	"net"
	"unsafe"
)

type Socket struct {
	name     string
	num      int
	conf     SocketConfig
	fd       int
	newConns chan *net.UnixConn
	conns    map[*net.UnixConn]bool
	ring     unsafe.Pointer
  ringUse []int
}

func newSocket(sc SocketConfig, fanoutID int, name string, num int) (*Socket, error) {
	s := &Socket{
		name: name,
		num:  num,
		conf: sc,
    newConns: make(chan *net.UnixConn),
    conns: map[*net.UnixConn]bool{},
    ringUse: make([]int, sc.NumBlocks),
	}
  iface := C.CString(sc.Interface)
  defer C.free(unsafe.Pointer(iface))
  var fd C.int
  if _, err := C.AFPacket(iface, C.int(sc.BlockSize), C.int(sc.NumBlocks),
      C.int(sc.BlockTimeoutMillis), C.int(fanoutID), C.int(sc.FanoutType),
      &fd, &s.ring); err != nil {
    return nil, err
  }
	return s, nil
}

func (s *Socket) run() {
	for {
		select {
		case c := <-s.newConns:
			s.addNewConn(c)
		default:
		}
	}
}

func (s *Socket) addNewConn(c *net.UnixConn) {
	log.Println("new connection on socket %q", s.name)
}
