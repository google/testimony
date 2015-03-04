package main

import (
	"log"
	"net"
)

type Config struct {
	Sockets map[string]SocketConfig
}

type SocketConfig struct {
	BlockSize          int
	NumBlocks          int
	BlockTimeoutMillis int
	FanoutType         int
}

type Testimony struct {
	sockets map[string]*Socket
}

func RunTestimony(c Config) {
	t := &Testimony{
		sockets: map[string]*Socket{},
	}
	for name, sc := range c.Sockets {
		if t.sockets[name] != nil {
			log.Fatal("invalid config: duplicate socket name %q", name)
		}
		sock, err := newSocket(sc)
		if err != nil {
			log.Fatal("invalid config %+v: %v", sc, err)
		}
		t.sockets[name] = sock
		go sock.run()
	}
	t.run()
}

func (t *Testimony) run() {
	list, err := net.Listen("unixpacket", ".testimony_socket")
	if err != nil {
		log.Fatal(err)
	}
	for {
		c, err := list.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go t.handle(c.(*net.UnixConn))
	}
}

func (t *Testimony) handle(c *net.UnixConn) {
	log.Println("handling new connection %p", c)
	var name [100]byte
	n, err := c.Read(name[:])
	if err != nil {
		log.Println("new conn failed to send name: %v", err)
		c.Close()
		return
	}
	socket := t.sockets[string(name[:n])]
	if socket == nil {
		log.Println("new conn requested invalid name %q", string(name[:n]))
		c.Close()
		return
	}
	socket.newConns <- c
}

func main() {
}
