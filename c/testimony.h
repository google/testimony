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

typedef struct {
  int sock_fd;
  int afpacket_fd;
  char* ring;
  int block_size;
  int block_nr;
} testimony;

// The following functions return 0 on success, -errno on failure.

// Initializes a connection to the testimony server.
int testimony_init(testimony* t, const char* socket_name, int num);
// Closes a connection to the testimony server.
int testimony_close(testimony* t);
// Gets a new block of packets from testimony.
int testimony_get_block(testimony* t, struct tpacket_block_desc** block);
// Returns a processed block of packets back to testimony.
int testimony_return_block(testimony* t, struct tpacket_block_desc* block);

#ifdef __cplusplus
}
#endif

#endif
