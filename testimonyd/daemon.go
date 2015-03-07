package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"os"
	"syscall"
)

var confFilename = flag.String("config", "/etc/testimony.conf", "Testimony config")

type Config struct {
	ListenSocket string
	Sockets      map[string]SocketConfig
}

type SocketConfig struct {
	Interface          string
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
	conf    Config
	sockets map[string][]*Socket
}

func RunTestimony(c Config) {
	t := &Testimony{
		conf:    c,
		sockets: map[string][]*Socket{},
	}
	fanoutID := 0
	for name, sc := range c.Sockets {
		fanoutID++
		if t.sockets[name] != nil {
			log.Fatalf("invalid config: duplicate socket name %q", name)
		}
		for i := 0; i < sc.FanoutSize; i++ {
			sock, err := newSocket(sc, fanoutID, name, i)
			if err != nil {
				log.Fatalf("invalid config %+v: %v", sc, err)
			}
			t.sockets[name] = append(t.sockets[name], sock)
			go sock.run()
		}
	}
	t.run()
}

func (t *Testimony) run() {
	os.Remove(t.conf.ListenSocket) // ignore errors
	list, err := net.ListenUnix("unix", &net.UnixAddr{Net: "unix", Name: t.conf.ListenSocket})
	if err != nil {
		log.Fatalf("failed to listen on socket: %v", err)
	}
	for {
		c, err := list.Accept()
		if err != nil {
			log.Fatalf("failed to accept connection: %v", err)
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
	v(1, "handling new connection %p", c)
	var buf [1024]byte
	n, err := c.Read(buf[:])
	if err != nil {
		log.Printf("new conn failed to read conf: %v", err)
		return
	}
	var req Request
	if err := json.NewDecoder(bytes.NewBuffer(buf[:n])).Decode(&req); err != nil {
		log.Printf("new conn request could not be decoded: %v", err)
		return
	}
	socks := t.sockets[req.Name]
	if socks == nil {
		log.Printf("new conn requested invalid name %q", req.Name)
		return
	} else if len(socks) <= req.Num || req.Num < 0 {
		log.Printf("new conn requested invalid num %d (we have %d)", req.Num, len(socks))
		return
	}
	sock := socks[req.Num]
	fdMsg := syscall.UnixRights(sock.fd)
	var msg [8]byte
	binary.BigEndian.PutUint32(msg[0:4], uint32(sock.conf.BlockSize))
	binary.BigEndian.PutUint32(msg[4:8], uint32(sock.conf.NumBlocks))
	n, n2, err := c.WriteMsgUnix(
		msg[:], fdMsg, nil)
	if err != nil || n != len(msg) || n2 != len(fdMsg) {
		log.Printf("new conn failed to send file descriptor: %v", err)
		return
	}
	v(1, "new conn spun up, passing off to socket")
	sock.newConns <- c
	c = nil // so it doesn't get closed by deferred func.
}

func main() {
	flag.Parse()
	v(1, "Starting testimonyd...")
	confdata, err := ioutil.ReadFile(*confFilename)
	if err != nil {
		log.Fatalf("could not read configuration %q: %v", *confFilename, err)
	}
	var config Config
	if err := json.NewDecoder(bytes.NewBuffer(confdata)).Decode(&config); err != nil {
		log.Fatalf("could not parse configuration %q: %v", *confFilename, err)
	}
	RunTestimony(config)
}
