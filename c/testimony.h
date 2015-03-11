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

#ifndef __TESTIMONY_H__
#define __TESTIMONY_H__

#include <linux/if_packet.h>
#include <stdint.h>  // int64_t

#ifdef __cplusplus
extern "C" {
#endif

// All functions that return an int return 0 on success, -errno on failure.

struct testimony_internal;
// testimony provides a link to a local testimony server, which serves up
// AF_PACKET packet blocks.  Usage:
//
//   testimony t;
//   CHECK(testimony_init(&t, "/tmp/socketname", 0) == 0);
//   struct tpacket_block_desc* block;
//   CHECK(testimony_get_block(t, 1000 /* 1 sec */, &block) == 0);
//   // use block...
//   CHECK(testimony_return_block(t, block) == 0);
//   // call get/return more times if you want.
//   CHECK(testimony_close(t) == 0);
typedef struct testimony_internal* testimony;

typedef struct {
  // Filled in by server, shouldn't be modified by client:
  int fanout_size;    // set by testimony_connect
  size_t block_size;  // set by testimony_init
  size_t block_nr;    // set by testimony_init
  // Settable by client to modify behavior of testimony_init:
  int fanout_index;
} testimony_connection;

// Initializes a connection to the testimony server.
int testimony_connect(testimony* t, const char* socket_name);
// Requests information about the connection.  This can be called after connect
// and before init.  All other functions should be called after init.
// Modifications to the returned data will modify the behavior of init.
testimony_connection* testimony_conn(testimony t);
// Returns a human-readable error message related to the last issue.
char* testimony_error(testimony t);
// Initiates block reads.  The behavior of this function will change
// based on modifications the client has made to testimony_conn(t).
int testimony_init(testimony t);
// Closes a connection to the testimony server.  t should not be reused after
// this call.
int testimony_close(testimony t);
// Gets a new block of packets from testimony.
// If timeout_millis < 0, block forever.  If == 0, don't block.  If > 0, block
// for at most the given number of milliseconds.
int testimony_get_block(testimony t, int timeout_millis, struct tpacket_block_desc** block);
// Returns a processed block of packets back to testimony.
int testimony_return_block(testimony t, struct tpacket_block_desc* block);

struct testimony_iter_internal;
// testimony_iter provides an easy method for iterating over packets
// in a tpacket3 block.
//
// Usage:
//   testimony_iter iter;
//   CHECK(testimony_iter_init(&iter) == 0);
//   while (...) {
//     struct tpacket_block_desc* block = get_block_somehow();
//     CHECK(testimony_iter_start(iter, block) == 0);
//     struct tpacket3_hdr* packet;
//     while ((packet = testimony_iter_next(iter) != NULL) {
//       handle_packet(packet);
//     }
//   }
//   CHECK(testimony_iter_close(iter));
//
typedef struct testimony_iter_internal* testimony_iter;

// Initiate iterator.  Returns 0 on success, -errno on failure.
int testimony_iter_init(testimony_iter* iter);
// Reset iterator to iterate over a new block.
int testimony_iter_reset(
    testimony_iter iter, struct tpacket_block_desc* block);
// Return the next packet in the block, or NULL if we have no more packets.
struct tpacket3_hdr* testimony_iter_next(testimony_iter iter);
// Clean up the iterator.  Use of iter after this call will break.
int testimony_iter_close(testimony_iter iter);

// testimony_packet_data is a helper function to extract the packet data
// from a tpacket3 packet header.  The returned buffer will be pkt->tp_snaplen
// bytes long.  pkt->tp_len is the length of the original packet, and may
// be >= tp_snaplen.
uint8_t* testimony_packet_data(struct tpacket3_hdr* pkt);
// testimony_packet_nanos is the nanosecond timestamp for the given packet.
int64_t testimony_packet_nanos(struct tpacket3_hdr* pkt);

#ifdef __cplusplus
}
#endif

#endif
