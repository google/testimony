package testimony

// #include <linux/if_packet.h>
import "C"

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func localSocketName() string {
	var randbytes [8]byte
	if n, err := rand.Read(randbytes[:]); err != nil || n != len(randbytes) {
		panic("random bytes failure")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("testimony_go_client_%s", hex.EncodeToString(randbytes[:])))
}

type Testimony struct {
	c                    *net.UnixConn
	fd                   int
	ring                 []byte
	numBlocks, blockSize int
}

func (t *Testimony) Close() (ret error) {
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

type Block struct {
	t      *Testimony
	i      int
	B      []byte
	offset int
	left   int
	pkt    *C.struct_tpacket3_hdr
}

func Connect(socketname string, num int) (*Testimony, error) {
	t := &Testimony{}
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
  if n, err := t.c.Write([]byte{byte(num)}); err != nil || n != 1 {
		return nil, fmt.Errorf("error writing initial request: %v", err)
	}
	var msg [2]byte
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
	t.blockSize = 1 << uint(msg[0])
	t.numBlocks = int(msg[1])
	if t.ring, err = syscall.Mmap(t.fd, 0, t.blockSize*t.numBlocks, syscall.PROT_READ, syscall.MAP_SHARED|syscall.MAP_LOCKED|syscall.MAP_NORESERVE); err != nil {
		return nil, fmt.Errorf("mmap failed: %v", err)
	}
	done = true
	return t, nil
}

func (t *Testimony) Block() (*Block, error) {
	var m [1]byte
	if n, err := t.c.Read(m[:]); err != nil || n != 1 {
		return nil, fmt.Errorf("error reading block index: %v", err)
	}
	idx := int(m[0])
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

func (b *Block) Close() error {
	m := [1]byte{byte(b.i)}
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

func (b *Block) Packet() *C.struct_tpacket3_hdr {
	return b.pkt
}
