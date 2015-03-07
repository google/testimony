#include <testimony.h>
#include <stdio.h>   // fprintf()
#include <string.h>  // strerror()
#include <stdlib.h>  // atoi()

int main(int argc, char** argv) {
  int r;
  struct tpacket_block_desc* block;
  int num;
  testimony t;

  if (argc != 4) {
    fprintf(stderr, "Usage: %s <socket> <name> <num>\n", argv[0]);
  }
  num = atoi(argv[3]);

  printf("Init...\n");
  r = testimony_init(&t, argv[1], argv[2], num);
  if (r < 0) {
    fprintf(stderr, "Error with init: %s\n", strerror(-r));
    return 1;
  }
  printf("Init complete\n");
  while (1) {
    r = testimony_get_block(&t, &block);
    if (r < 0) {
      fprintf(stderr, "Error with get: %s\n", strerror(-r));
      return 1;
    }
  }
  return 0;
}
