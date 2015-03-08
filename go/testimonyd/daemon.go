package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

var confFilename = flag.String("config", "/etc/testimony.conf", "Testimony config")

type Testimony []SocketConfig

const protocolVersion = 1

type SocketConfig struct {
	SocketName         string
	Interface          string
	BlockSizePowerOf2  int
	NumBlocks          int
	BlockTimeoutMillis int
	FanoutType         int
	FanoutSize         int
	User               string
}

func (s SocketConfig) uid() (int, error) {
	var u *user.User
	var err error
	if s.User == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(s.User)
	}
	if err != nil {
		return 0, fmt.Errorf("could not get user: %v", err)
	}
	return strconv.Atoi(u.Uid)
}

func (s SocketConfig) blockSize() int {
	return 1 << uint(s.BlockSizePowerOf2)
}

type Request struct {
	Name string
	Num  int
}

func RunTestimony(t Testimony) {
	fanoutID := 0
	names := map[string]bool{}
	for _, sc := range t {
		if names[sc.SocketName] {
			log.Fatalf("invalid config: duplicate socket name %q", sc.SocketName)
		}
		names[sc.SocketName] = true
		var socks []*Socket
		fanoutID++
		for i := 0; i < sc.FanoutSize; i++ {
			sock, err := newSocket(sc, fanoutID, i)
			if err != nil {
				log.Fatalf("invalid config %+v: %v", sc, err)
			}
			socks = append(socks, sock)
			go sock.run()
		}
		os.Remove(sc.SocketName) // ignore errors
		list, err := net.ListenUnix("unix", &net.UnixAddr{Net: "unix", Name: sc.SocketName})
		if err != nil {
			log.Fatalf("failed to listen on socket: %v", err)
		}
		if err := setPermissions(sc); err != nil {
			log.Fatalf("failed to set socket permissions: %v", err)
		}
		go t.run(list, sc, socks)
	}
	// We'd love to drop privs here, but thanks to
	// https://github.com/golang/go/issues/1435 we can't :(
	select {}
}

func setPermissions(sc SocketConfig) error {
	uid, err := sc.uid()
	if err != nil {
		return fmt.Errorf("could not get uid to change to: %v", err)
	}
	v(1, "chowning %q to %d", sc.SocketName, uid)
	if err := syscall.Chown(sc.SocketName, uid, 0); err != nil {
		return fmt.Errorf("unable to chown to (%d, 0): %v", uid, err)
	}
	return nil
}

func (t Testimony) run(list *net.UnixListener, sc SocketConfig, socks []*Socket) {
	for {
		c, err := list.AcceptUnix()
		if err != nil {
			log.Fatalf("failed to accept connection: %v", err)
		}
		go t.handle(socks, c)
	}
}

func (t Testimony) handle(socks []*Socket, c *net.UnixConn) {
	defer func() {
		if c != nil {
			c.Close()
		}
	}()
	v(1, "Received new connection %v", c.RemoteAddr())
	buf := [1]byte{protocolVersion}
	if n, err := c.Write(buf[:]); n != 1 || err != nil {
		log.Printf("new conn failed to write version: %v", err)
		return
	}
	if n, err := c.Read(buf[:]); n != 1 || err != nil {
		log.Printf("new conn failed to read conf: %v", err)
		return
	}
	idx := int(buf[0])
	if idx < 0 || idx >= len(socks) {
		log.Printf("new conn invalid index %v", idx)
		return
	}
	sock := socks[idx]
	fdMsg := syscall.UnixRights(sock.fd)
	msg := []byte{byte(sock.conf.BlockSizePowerOf2), byte(sock.conf.NumBlocks)}
	n, n2, err := c.WriteMsgUnix(
		msg[:], fdMsg, nil)
	if err != nil || n != len(msg) || n2 != len(fdMsg) {
		log.Printf("new conn failed to send file descriptor: %v", err)
		return
	}
	v(2, "new conn spun up, passing off to socket")
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
	// Set umask which will affect all of the sockets we create:
	syscall.Umask(0177)
	var t Testimony
	if err := json.NewDecoder(bytes.NewBuffer(confdata)).Decode(&t); err != nil {
		log.Fatalf("could not parse configuration %q: %v", *confFilename, err)
	}
	RunTestimony(t)
}
