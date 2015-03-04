#include <arpa/inet.h>   // htons()
#include <stdio.h>       // printf()
#include <sys/socket.h>  // socket(), bind(), listen(), accept(), SOCK_STREAM
#include <sys/types.h>   // See socket man page
#include <sys/un.h>      // sockaddr_un
#include <string.h>      // strncpy(), strerror()
#include <unistd.h>      // unlink(), getpid()
#include <linux/if_ether.h>   // ETH_P_ALL
#include <linux/if_packet.h>  // TPACKET_V3, PACKET_FANOUT_LB
#include <stdlib.h>           // exit()
#include <errno.h>            // errno
#include <net/if.h>           // if_nametoindex()
#include <sys/mman.h>         // mmap(), PROT_*, MAP_*

void Error(const char* file, int line) {
  fprintf(stderr, "FAILED AT %s:%d: %s\n", file, line, strerror(errno));
  exit(1);
}

int CheckForError(const char* file, int line, int x) {
  if (x < 0) {
    Error(file, line);
  }
  return x;
}

#define CHKERR(x) CheckForError(__FILE__, __LINE__, x)
#define IGNORE(x) x;
#define ERR() Error(__FILE__, __LINE__)

#ifndef UNIX_PATH_MAX
#define UNIX_PATH_MAX 108
#endif

int AFPacket(const char* iface, int block_size, int block_nr, int block_ms,
             int fanout_id, int fanout_type, int* fd, void** ring) {
  fd = socket(AF_PACKET, SOCK_RAW, htons(ETH_P_ALL));
  if (fd < 0) {
    return -1;
  }

  int v = TPACKET_V3;
  int r = setsockopt(fd, SOL_PACKET, PACKET_VERSION, &v, sizeof(v));
  if (r < 0) {
    goto fail1;
  }

  tpacket_req3 tp3;
  memset(&tp3, 0, sizeof(tp3));
  tp3.tp_block_size = block_size;
  tp3.tp_frame_size = block_size;
  tp3.tp_block_nr = block_nr;
  tp3.tp_frame_nr = block_nr;
  tp3.tp_retire_blk_tov = block_ms;  // timeout, ms
  r = setsockopt(fd, SOL_PACKET, PACKET_RX_RING, &tp3, sizeof(tp3));
  if (r < 0) {
    goto fail1;
  }

  *ring =
      mmap(NULL, tp3.tp_block_size * tp3.tp_block_nr, PROT_READ | PROT_WRITE,
           MAP_SHARED | MAP_LOCKED | MAP_NORESERVE, fd, 0);
  if (*ring == MAP_FAILED) {
    errno = EINVAL;
    goto fail1;
  }

  struct sockaddr_ll ll;
  memset(&ll, 0, sizeof(ll));
  ll.sll_family = AF_PACKET;
  ll.sll_protocol = htons(ETH_P_ALL);
  ll.sll_ifindex = if_nametoindex(iface);
  if (ll.sll_ifindex == 0) {
    ERR();
  }
  r = bind(fd, (struct sockaddr*)&ll, sizeof(ll));
  if (r < 0) {
    goto fail2;
  }

  int fanout = (fanout_id & 0xFFFF) | (fanout_type << 16);
  r = setsockopt(fd, SOL_PACKET, PACKET_FANOUT, &fanout, sizeof(fanout));
  if (r < 0) {
    goto fail2;
  }
  return 0;

fail2 : {
  int err = errno;
  munmap(*ring);
  errno = err;
}
fail1 : {
  int err = errno;
  close(*fd);
  errno = err;
}
  return -1;
}
