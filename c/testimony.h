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

#ifdef __cplusplus
extern "C" {
#endif

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

// The following functions return 0 on success, -errno on failure.

// Initializes a connection to the testimony server.
int testimony_init(testimony* t, const char* socket_name, int num);
// Closes a connection to the testimony server.  t should not be reused after
// this call.
int testimony_close(testimony t);
// Gets a new block of packets from testimony.
// If timeout_millis < 0, block forever.  If == 0, don't block.  If > 0, block
// for at most the given number of milliseconds.
int testimony_get_block(testimony t, int timeout_millis, struct tpacket_block_desc** block);
// Returns a processed block of packets back to testimony.
int testimony_return_block(testimony t, struct tpacket_block_desc* block);
// Returns the size of an individual block, in bytes.
int testimony_block_size(testimony t);
// Returns the number of blocks.
int testimony_block_nr(testimony t);

#ifdef __cplusplus
}
#endif

#endif
