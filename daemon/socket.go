package main

import "C"

import (
	"log"
	"net"
)

type Socket struct {
	name     string
	conf     SocketConfig
	fd       int
	newConns chan *net.UnixConn
	conns    map[*net.UnixConn]bool
}

func newSocket(sc SocketConfig) (*Socket, error) {
	return nil, nil
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
