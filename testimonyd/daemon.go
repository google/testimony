package main

import (
  "bytes"
	"encoding/json"
	"log"
	"net"
)

type Config struct {
	Sockets map[string]SocketConfig
}

type SocketConfig struct {
  Interface string
	BlockSize          int
	NumBlocks          int
	BlockTimeoutMillis int
	FanoutType         int
	FanoutSize         int
}

type Request struct {
	Name string
	Num  int
}

type Testimony struct {
	sockets map[string][]*Socket
}

func RunTestimony(c Config) {
	t := &Testimony{
		sockets: map[string][]*Socket{},
	}
  fanoutID := 0
	for name, sc := range c.Sockets {
    fanoutID++
		if t.sockets[name] != nil {
			log.Fatal("invalid config: duplicate socket name %q", name)
		}
		for i := 0; i < sc.FanoutSize; i++ {
			sock, err := newSocket(sc, fanoutID, name, i)
			if err != nil {
				log.Fatal("invalid config %+v: %v", sc, err)
			}
			t.sockets[name] = append(t.sockets[name], sock)
			go sock.run()
		}
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
	defer func() {
		if c != nil {
			c.Close()
		}
	}()
	log.Println("handling new connection %p", c)
	var buf [100]byte
	n, err := c.Read(buf[:])
	if err != nil {
		log.Println("new conn failed to read conf: %v", err)
		return
	}
	var req Request
	if err := json.NewDecoder(bytes.NewBuffer(buf[:n])).Decode(&req); err != nil {
		log.Println("new conn request could not be decoded: %v", err)
		return
	}
	socks := t.sockets[req.Name]
	if socks == nil {
		log.Println("new conn requested invalid name %q", req.Name)
		return
	} else if len(socks) <= req.Num || req.Num < 0 {
		log.Println("new conn requested invalid num %d (we have %d)", req.Num, len(socks))
		return
	}
	socks[req.Num].newConns <- c
	c = nil // so it doesn't get closed by deferred func.
}

func main() {
}
