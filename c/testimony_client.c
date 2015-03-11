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

int main(int argc, char** argv) {
  int r, i;
  struct tpacket_block_desc* block;
  testimony t;

  if (argc != 2) {
    fprintf(stderr, "Usage: %s <socket>\n", argv[0]);
  }

  printf("Init...\n");
  r = testimony_connect(&t, argv[1]);
  if (r < 0) {
    fprintf(stderr, "Error with connect: %s\n", strerror(-r));
    return 1;
  }
  r = testimony_init(t);
  if (r < 0) {
    fprintf(stderr, "Error with init: %s\n", strerror(-r));
    return 1;
  }
  printf("Init complete\n");
  for (i = 0; i < 5; i++) {
    r = testimony_get_block(t, -1, &block);
    if (r < 0) {
      fprintf(stderr, "Error with get: %s\n", strerror(-r));
      return 1;
    }
    printf("%d\tgot block %p with %d packets\n", i, block,
           block->hdr.bh1.num_pkts);
    r = testimony_return_block(t, block);
    if (r < 0) {
      fprintf(stderr, "Error with return: %s\n", strerror(-r));
      return 1;
    }
  }
  testimony_close(t);
  return 0;
}
