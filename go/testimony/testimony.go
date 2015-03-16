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

// Package testimony provides a method for sharing AF_PACKET memory regions
// across multiple processes.
package testimony

// #include <linux/if_packet.h>
import "C"

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const protocolVersion = 1

func localSocketName() string {
	var randbytes [8]byte
	if n, err := rand.Read(randbytes[:]); err != nil || n != len(randbytes) {
		panic("random bytes failure")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("testimony_go_client_%s", hex.EncodeToString(randbytes[:])))
}

// Conn is a connection to the testimonyd server.  It allows the current process
// to share testimonyd AF_PACKET sockets.
type Conn struct {
	c                    *net.UnixConn
	fd                   int
	ring                 []byte
	numBlocks, blockSize int
}

// Close closes the connection to the testimonyd server.
func (t *Conn) Close() (ret error) {
	if t.ring != nil {
		if err := syscall.Munmap(t.ring); err != nil {
			ret = err
		} else {
			t.ring = nil
		}
	}
	if t.fd != 0 {
		if err := syscall.Close(t.fd); err != nil {
			ret = err
		} else {
			t.fd = 0
		}
	}
	if t.c != nil {
		if err := t.c.Close(); err != nil {
			ret = err
		} else {
			t.c = nil
		}
	}
	return
}

// Block is an AF_PACKET TPACKETv3 block, and provides access to the packets in
// that block.
type Block struct {
	t      *Conn
	i      int
	B      []byte
	offset int
	left   int
	pkt    *C.struct_tpacket3_hdr
}

// Connect connects to the testimonyd server.
func Connect(socketname string, num int) (*Conn, error) {
	t := &Conn{}
	done := false
	defer func() {
		if !done {
			t.Close()
		}
	}()
	var err error
	t.c, err = net.DialUnix("unix",
		&net.UnixAddr{Net: "unix", Name: localSocketName()},
		&net.UnixAddr{Net: "unix", Name: socketname})
	if err == nil {
		return nil, fmt.Errorf("error connecting: %v", err)
	}
	var initial [13]byte
	if n, err := t.c.Read(initial[:]); err != nil || n != len(initial) {
		return nil, fmt.Errorf("error reading initial byte: %v", err)
	} else if initial[0] != protocolVersion {
		return nil, fmt.Errorf("protocol mismatch, want %v got %v", protocolVersion, initial[0])
	}
	_ = int(binary.BigEndian.Uint32(initial[1:]))
	t.blockSize = int(binary.BigEndian.Uint32(initial[5:]))
	t.numBlocks = int(binary.BigEndian.Uint32(initial[9:]))
	// TODO:  Parse fanout size, allow client to chose fanout number based on it.
	var fanoutNum [4]byte
	binary.BigEndian.PutUint32(fanoutNum[:], uint32(num))
	if _, err := t.c.Write(fanoutNum[:]); err != nil {
		return nil, fmt.Errorf("error writing initial request: %v", err)
	}
	var msg [1]byte
	var oob [1024]byte
	n, n2, _, _, err := t.c.ReadMsgUnix(msg[:], oob[:])
	if err != nil {
		return nil, fmt.Errorf("error reading fd: %v", err)
	} else if n != len(msg) {
		return nil, fmt.Errorf("got wrong number of initial bytes: %d", n)
	} else if n2 >= len(oob) {
		return nil, fmt.Errorf("got too many oob bytes: %d", n2)
	}
	if msgs, err := syscall.ParseSocketControlMessage(oob[:n2]); err != nil {
		return nil, fmt.Errorf("could not parse socket control msg: %v", err)
	} else if len(msgs) != 1 {
		return nil, fmt.Errorf("wrong number of control messages: %d", len(msgs))
	} else if fds, err := syscall.ParseUnixRights(&msgs[0]); err != nil {
		return nil, fmt.Errorf("could not parse unix rights: %v", err)
	} else if len(fds) != 1 {
		return nil, fmt.Errorf("wrong number of fds: %d", len(fds))
	} else {
		t.fd = fds[0]
	}
	if t.ring, err = syscall.Mmap(t.fd, 0, t.blockSize*t.numBlocks, syscall.PROT_READ, syscall.MAP_SHARED|syscall.MAP_LOCKED|syscall.MAP_NORESERVE); err != nil {
		return nil, fmt.Errorf("mmap failed: %v", err)
	}
	done = true
	return t, nil
}

// Block gets the next block of packets from testimonyd.
func (t *Conn) Block() (*Block, error) {
	var m [4]byte
	if n, err := t.c.Read(m[:]); err != nil || n != len(m) {
		return nil, fmt.Errorf("error reading block index: %v", err)
	}
	idx := int(binary.BigEndian.Uint32(m[:]))
	if idx < 0 || idx >= t.numBlocks {
		return nil, fmt.Errorf("read invalid index %d", idx)
	}
	start := idx * t.blockSize
	return &Block{
		t: t,
		i: idx,
		B: t.ring[start : start+t.blockSize],
	}, nil
}

// Return returns this block to the testimonyd server.
func (b *Block) Return() error {
	var m [4]byte
	binary.BigEndian.PutUint32(m[:], uint32(b.i))
	if _, err := b.t.c.Write(m[:]); err != nil {
		return fmt.Errorf("error writing index: %v", err)
	}
	b.t, b.i, b.B = nil, 0, nil
	return nil
}

func (b *Block) header() *C.struct_tpacket_hdr_v1 {
	desc := (*C.struct_tpacket_block_desc)(unsafe.Pointer(&b.B[0]))
	return (*C.struct_tpacket_hdr_v1)(unsafe.Pointer(&desc.hdr[0]))
}

// Next allows the user to iterate through the set of packets in this Block,
// changing the value returned by Packet.
func (b *Block) Next() bool {
	if b.offset == 0 {
		b.left = int(b.header().num_pkts)
		b.offset = int(b.header().offset_to_first_pkt)
	} else {
		b.offset += int(b.pkt.tp_next_offset)
	}
	if b.left <= 0 {
		return false
	}
	b.left--
	b.pkt = (*C.struct_tpacket3_hdr)(unsafe.Pointer(&b.B[b.offset]))
	return true
}

// Packet provides access to the current packet.  Next calls change this to
// point to the next packet in the block.
func (b *Block) Packet() *C.struct_tpacket3_hdr {
	return b.pkt
}
