[![Build Status](https://travis-ci.org/google/testimony.svg?branch=master)](https://travis-ci.org/google/testimony)

Testimony
=========

Testimony is a single-machine, multi-process architecture for sharing AF_PACKET
data across processes.  This allows packets to be copied from NICs into memory
a single time.  Then, multiple processes can process this packet data in
parallel without the need for additional copies.

Testimony allows users to configure multiple different AF_PACKET sockets with
different filters, blocks, etc.  Administrators can specify BPF filters for
these sockets, as well as which user should have access to which socket.  This
allows admins to easily set up access to a restricted set of packets for
specific users.

For discussion, questions, etc, use testimony-discuss@googlegroups.com.

Detailed Design
---------------

Testimony is implemented in a very simple client/server model.  A single server,
`testimonyd`, creates AF_PACKET sockets.  Client processes then talk to it over
AF_UNIX sockets.  The client processes are passed the socket file descriptors;
each then mmap's the socket into its own memory space.  This done, the server
then watches for new packet blocks and serves indexes to those blocks out to
each client, reference-counting the blocks and returning them to the kernel only
when all clients have released them.

### Configuration ###

On creation, sockets are configured based on the /etc/testimony.conf
configuration file, which lists all sockets to be created.  Each socket contains
these configuration options:

*   **SocketName:**  Name of the socket file to create.  `/tmp/foo.sock`, that kind
     of thing.  This socket name is given to a connecting client so it can find
     where/how to communicate with `testimonyd`.
*   **Interface:**  Name of the interface to sniff packets on, e.g. `eth0`, `em1`,
     etc.
*   **BlockSize:**  AF_PACKET provides packets to user-space by filling up
     memory blocks of a specific size, until it either can't fit the next packet
     into the current block or a timeout is reached.  The larger the block, the
     more packets can be passed to the user at once.  BlockSize is in bytes.
*   **FrameSize:** Number of frames. Frames are grouped in blocks. If FrameSize 
     is a divisor of tp_block_size frames will be contiguously spaced by 'FrameSize'
     bytes. If not, there will be a gap between the frames in blocks. This is because
     a frame cannot be spawn across two blocks. 
*   **NumBlocks:**  Number of blocks to allocate in memory.  `NumBlocks *
     BlockSize` is the total size in memory of the AF_PACKET packet memory
     region for a single fanout.
*   **BlockTimeoutMillis:**  If fewer than BlockSize bytes are sniffed by AF_PACKET
     before this number of milliseconds passes, AF_PACKET provides the current
     block to users in a less-than-full state.
*   **FanoutType:**  AF_PACKET allows fanout, where multiple memory regions of the
     same size are created and packets are load-balanced between them.  For
     fanout possibilities, see `/usr/include/linux/if_packet.h`
*   **FanoutSize:**  The number of memory regions to fan out to.  Total memory
     usage of AF_PACKET is `FanoutSize * MemoryRegionSize`, where
     `MemoryRegionSize` is `BlockSize * NumBlocks`.  FanoutSize can be
     considered the number of parallel processes that want to access packet
     data.
*   **FanoutID:**  Integer fanout ID to use when setting socket options. These
     are globally unique so it can be tuned to avoid conflicts with other
     processes that use AF_PACKET. If unspecified or 0 an ID will be auto
     assigned starting with 1.
*   **User:** This socket will be owned by the given user, mode `0600`.  This
     allows root to provide different sockets with different capabilities to
     specific users.
*   **Filter:** BPF filter for this socket.  If this is set, testimony will
     guarantee that the socket passed to child processes has this filter locked
     in such a way that clients cannot remove it.

### Wire Protocol ###

Testimony uses an extremely simple wire protocol for establishing client
connections and passing memory regions back and forth.

Most values are passed as TLV, in the form:

```
| 0 | 1 | 2 | 3 | 4 | 5 | 6 | ... |
| type  | len   | value ....      |
```

Where type and length are big-endian uint32, and value is a set of bytes `len`
bytes long.  The type will always have its highest-order bit set, to
differentiate between a TLV and a block index.

    SERVER                                              CLIENT
    ------                                              ------
             <-- initial connection ---
             --- version byte (1 byte == 2) --->
             --- fanout size, block size, num blocks -->
             --- waiting for fanout index -->
             <-- fanout index ---
             --- socket FD, + 1 dummy byte (ignored) -->

              At this point, the client is connected.

             --- block index for client (4BE) -->
             <-- block index to return (4BE) ---

Post-connection, most communication is 4-byte block indexes passed back
and forth.  At any time post-connection, either the server or client may
send arbitrary TLV values across the wire... the other side should handle
them if it knows how and ignore them if it doesn't.

The server sends a block index to the client when that block is
available to process (and it references the block internally).  The client
returns the block index to the server when it's done processing it, and the
server unrefs that block.  When a block has no more references, it is returned
to the kernel to be refilled with packets.


### Installation ###

Run install.sh after testimony building testimony.
This script will create default config file `/etc/testimony.conf` and will add new testimony service.