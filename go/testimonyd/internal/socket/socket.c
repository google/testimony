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

#include <arpa/inet.h>   // htons()
#include <sys/socket.h>  // socket(), bind(), listen(), accept(), SOCK_STREAM
#include <sys/types.h>   // See socket man page
#include <string.h>      // strerror()
#include <linux/if_ether.h>   // ETH_P_ALL
#include <linux/if_packet.h>  // TPACKET_V3, PACKET_FANOUT_LB
#include <errno.h>            // errno
#include <net/if.h>           // if_nametoindex()
#include <sys/mman.h>         // mmap(), PROT_*, MAP_*
#include <unistd.h>           // close()
#include <linux/filter.h>     // sock_fprog, sock_filter

#ifndef UNIX_PATH_MAX
#define UNIX_PATH_MAX 108
#endif

// AFPacket does all of the necessary construction of an AF_PACKET socket
// in C, to avoid a bunch of C.blah cgo stuff in daemon.go.  It takes in a bunch
// of arguments and outputs an AF_PACKET socket file descriptor, a void*
// pointing to the mmap'd region of that socket, and any error message.
// Returns zero on success, on error returns -1 and sets errno.
int AFPacket(const char* iface, int block_size, int block_nr, int block_ms,
             int fanout_id, int fanout_size, int fanout_type, const struct sock_fprog* filter,
             // outputs:
             int* fd, void** ring, const char** err) {
  // Set up the initial socket.
  *fd = socket(AF_PACKET, SOCK_RAW, htons(ETH_P_ALL));
  if (*fd < 0) {
    *err = "socket creation failure";
    return -1;
  }

  // Request TPACKET_V3.
  int v = TPACKET_V3;
  int r = setsockopt(*fd, SOL_PACKET, PACKET_VERSION, &v, sizeof(v));
  if (r < 0) {
    *err = "setsockopt PACKET_VERSION failure";
    goto fail1;
  }

  // If requested, set up and lock a BPF filter on the socket.
  if (filter) {
#if defined(SO_ATTACH_FILTER) && defined(SO_LOCK_FILTER)
    r = setsockopt(*fd, SOL_SOCKET, SO_ATTACH_FILTER, filter, sizeof(*filter));
    if (r < 0) {
      *err = "setsockopt SO_ATTACH_FILTER error";
      goto fail1;
    }
    v = 1;
    r = setsockopt(*fd, SOL_SOCKET, SO_LOCK_FILTER, &v, sizeof(v));
    if (r < 0) {
      *err = "setsockopt SO_LOCK_FILTER error";
      goto fail1;
    }
#else
    // If folks want a filter, that means they want to give access to specific
    // packets to a specific user.  If we can't attach a filter, we give them
    // too many packets.  If we can't lock the filter, they can change the
    // filter on the socket they receive and elevate their permissions.  In
    // either case, fail hard.  If this isn't supported, folks can still use
    // testimonyd without filters.
    *err = "filter requested, but BPF filtering or filter locking unsupported";
    errno = ENOSYS;
    goto fail1;
#endif
  }

  // Request a RX_RING so we can mmap the socket.
  struct tpacket_req3 tp3;
  memset(&tp3, 0, sizeof(tp3));
  tp3.tp_block_size = block_size;
  tp3.tp_frame_size = block_size;
  tp3.tp_block_nr = block_nr;
  tp3.tp_frame_nr = block_nr;
  tp3.tp_retire_blk_tov = block_ms;  // timeout, ms
  r = setsockopt(*fd, SOL_PACKET, PACKET_RX_RING, &tp3, sizeof(tp3));
  if (r < 0) {
    *err = "setsockopt PACKET_RX_RING failure";
    goto fail1;
  }

  // MMap the RX_RING to create a packet memory region.
  *ring =
      mmap(NULL, (size_t) tp3.tp_block_size * tp3.tp_block_nr,
           PROT_READ | PROT_WRITE, MAP_SHARED | MAP_LOCKED | MAP_NORESERVE,
           *fd, 0);
  if (*ring == MAP_FAILED) {
    *err = "ring mmap failed";
    errno = EINVAL;
    goto fail1;
  }

  // Bind the socket to a single interface.
  struct sockaddr_ll ll;
  memset(&ll, 0, sizeof(ll));
  ll.sll_family = AF_PACKET;
  ll.sll_protocol = htons(ETH_P_ALL);
  ll.sll_ifindex = if_nametoindex(iface);
  if (ll.sll_ifindex == 0) {
    *err = "if_nametoindex failed";
    errno = EINVAL;
    goto fail2;
  }
  r = bind(*fd, (struct sockaddr*)&ll, sizeof(ll));
  if (r < 0) {
    *err = "bind failed";
    goto fail2;
  }

  // Set up fanout.
  // If fanout size is 1, there's no point in trying to set fanout.
  if (fanout_size != 1) {
    int fanout = (fanout_id & 0xFFFF) | (fanout_type << 16);
    r = setsockopt(*fd, SOL_PACKET, PACKET_FANOUT, &fanout, sizeof(fanout));
    if (r < 0) {
      *err = "setsockopt PACKET_FANOUT failed";
      goto fail2;
    }
  }
  return 0;

fail2 : {
  int err = errno;
  munmap(*ring, block_size * block_nr);
  errno = err;
}
fail1 : {
  int err = errno;
  close(*fd);
  errno = err;
}
  return -1;
}
