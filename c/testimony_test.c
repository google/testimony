#include <testimony.h>
#include <stdio.h>   // fprintf()
#include <string.h>  // strerror()

int main(int argc, char **argv) {
  char c;
  int r, i, zeros;
  testimony t;

  if (argc != 2) {
    fprintf(stderr, "Usage: %s <socketname>\n", argv[0]);
  }

  r = testimony_init(&t, argv[1], NULL);
  if (r < 0) {
    fprintf(stderr, "Error with init: %s\n", strerror(-r));
    return 1;
  }
  printf("Got init: %d\n", t.sock_fd);
  zeros = 0;
  for (i = 0; i < (16 << 20); i++) {
    c = ((char *)t.ring)[i];
    if (c == 0) {
      zeros++;
      continue;
    }
    if (zeros > 0) {
      if (zeros > 100) {
        printf("\n");
      }
      printf("{%d}", zeros);
      zeros = 0;
    }
    printf("%02X", c);
  }
  return 0;
}
