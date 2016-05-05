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

#include <testimony.h>
#include <stdio.h>   // fprintf()
#include <string.h>  // strerror()
#include <stdlib.h>  // atoi()
#include <argp.h>    // argp_parse()

#define SOCKET_BUF_SIZE 256
char* flag_socket = "/path/to/socket";
int flag_fanout_index = 0;
int flag_count = 0;
int flag_dump = 0;

int ParseOption(int key, char* arg, struct argp_state* state) {
  switch (key) {
    case 300:
      flag_socket = arg; break;
    case 301:
      flag_fanout_index = atoi(arg); break;
    case 302:
      flag_count = atoi(arg); break;
    case 303:
      flag_dump = 1; break;
  }
  return 0;
}

int main(int argc, char** argv) {
  int r;
  struct tpacket_block_desc* block;
  struct tpacket3_hdr* packet;
  const uint8_t *packet_data;
  const uint8_t *packet_data_limit;
  testimony t;

  const char* s = "STRING";
  const char* n = "NUM";
  struct argp_option options[] = {
    {"socket", 300, s, 0, "Socket to connect to"},
    {"index", 301, n, 0, "Fanout index to request"},
    {"count", 302, n, 0, "Number of packets to process before exiting"},
    {"dump", 303, 0, 0, "Dump packet hex to STDOUT"},
    {0},
  };
  struct argp argp = {options, &ParseOption};
  argp_parse(&argp, argc, argv, 0, 0, 0);

  fprintf(stderr, "Connecting to '%s'\n", flag_socket);
  r = testimony_connect(&t, flag_socket);
  if (r < 0) {
    fprintf(stderr, "Error with connect: %s\n", strerror(-r));
    return 1;
  }
  testimony_conn(t)->fanout_index = flag_fanout_index;
  r = testimony_init(t);
  if (r < 0) {
    fprintf(stderr, "Error with init: %s: %s\n", testimony_error(t),
            strerror(-r));
    return 1;
  }
  fprintf(stderr, "Init complete\n");
  testimony_iter iter;
  testimony_iter_init(&iter);
  while (1) {
    r = testimony_get_block(t, -1, &block);
    if (r < 0) {
      fprintf(stderr, "Error with get: %s: %s\n", testimony_error(t),
              strerror(-r));
      return 1;
    }
    fprintf(stderr, "got block %p with %d packets\n", block,
            block->hdr.bh1.num_pkts);
    testimony_iter_reset(iter, block);
    while ((packet = testimony_iter_next(iter)) != NULL) {
      if (flag_dump) {
        packet_data = testimony_packet_data(packet);
        packet_data_limit = packet_data + packet->tp_snaplen;
        for (; packet_data < packet_data_limit; packet_data++) {
          printf("%02x", *packet_data);
        }
        printf("\n");
      }
      if (--flag_count == 0) {
        goto done;
      }
    }
    r = testimony_return_block(t, block);
    if (r < 0) {
      fprintf(stderr, "Error with return: %s: %s\n", testimony_error(t),
              strerror(-r));
      return 1;
    }
  }
done:
  testimony_close(t);
  return 0;
}
