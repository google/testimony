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
*   **BlockSizePowerOf2:**  AF_PACKET provides packets to user-space by filling up
     memory blocks of a specific size, until it either can't fit the next packet
     into the current block or a timeout is reached.  The larger the block, the
     more packets can be passed to the user at once.  We allow blocks to be any
     power-of-2 size.  If `BlockSizePowerOf2` is 20, blocks will be `1<<20`
     bytes (1MB) each.
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
*   **User:** This socket will be owned by the given user, mode `0600`.  This
     allows root to provide different sockets with different capabilities to
     specific users.
*   **Filter:** BPF filter for this socket.

### Wire Protocol ###

Testimony uses an extremely simple wire protocol for establishing client
connections and passing memory regions back and forth:

    SERVER                                              CLIENT
    ------                                              ------
             <-- initial connection ---
             --- version byte (1 byte == 1) --->
             --- fanout size (4BE) -->
             --- block size (4BE) -->
             --- number of blocks (4BE) -->
             <-- fanout number [0-FanoutSize) (4BE) ---
             --- socket FD, + 1 dummy byte (ignored) -->

              At this point, the client is connected.
              All other communication is one of the
              following 2 possibilities:

             --- block index for client (4BE) -->
             <-- block index to return (4BE) ---

Most communication is 4-byte "packets" (4BE above means 4 bytes, big endian).
Pne excepion is the message that sends the socket FD from server to client.
That contains the FD itself, as well as 8 bytes, representing the block size
and number of blocks both 4BE as well.

Once communication is established, server and client simply exchange block
indexes.  The server sends a block index to the client when that block is
available to process (and it references the block internally).  The client
returns the block index to the server when it's done processing it, and the
client unrefs that block.  When a block has no more references, it is returned
to the kernel to be refilled with packets.

