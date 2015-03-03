extern "C" {
#include <ancillary.h>
}

#include <arpa/inet.h>  // htons()
#include <stdio.h>  // printf()
#include <sys/socket.h>  // socket(), bind(), listen(), accept(), SOCK_STREAM
#include <sys/types.h>  // See socket man page
#include <sys/un.h>  // sockaddr_un
#include <string.h>  // strncpy(), strerror()
#include <unistd.h>  // unlink(), getpid()
#include <linux/if_ether.h>  // ETH_P_ALL
#include <linux/if_packet.h>  // TPACKET_V3, PACKET_FANOUT_LB
#include <stdlib.h>  // exit()
#include <errno.h>  // errno
#include <net/if.h>  // if_nametoindex()
#include <sys/mman.h>  // mmap(), PROT_*, MAP_*

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
#define ERR() Error(__FILE__, __LINE__)

const char* kSocketName = ".testimony_socket";
#ifndef UNIX_PATH_MAX
#define UNIX_PATH_MAX 108
#endif

int AFPacket(const char* iface, void** ring) {
  int fd = CHKERR(socket(AF_PACKET, SOCK_RAW, htons(ETH_P_ALL)));

  int v = TPACKET_V3;
  CHKERR(setsockopt(fd, SOL_PACKET, PACKET_VERSION, &v, sizeof(v)));

  tpacket_req3 tp3;
  memset(&tp3, 0, sizeof(tp3));
  tp3.tp_block_size = 1 << 20;
  tp3.tp_frame_size = 1 << 20;
  tp3.tp_block_nr = 16;
  tp3.tp_frame_nr = tp3.tp_block_nr * (tp3.tp_block_size / tp3.tp_frame_size);
  tp3.tp_retire_blk_tov = 1000;  // timeout, ms
  CHKERR(setsockopt(fd, SOL_PACKET, PACKET_RX_RING, &tp3, sizeof(tp3)));

  *ring = mmap(NULL, tp3.tp_block_size * tp3.tp_block_nr,
      PROT_READ | PROT_WRITE, MAP_SHARED | MAP_LOCKED | MAP_NORESERVE,
      fd, 0);
  if (*ring == MAP_FAILED) { ERR(); }

  struct sockaddr_ll ll;
  memset(&ll, 0, sizeof(ll));
  ll.sll_family = AF_PACKET;
  ll.sll_protocol = htons(ETH_P_ALL);
  ll.sll_ifindex = if_nametoindex(iface);
  if (ll.sll_ifindex == 0) { ERR(); }
  CHKERR(bind(fd, (struct sockaddr*)&ll, sizeof(ll)));

  int fanout = (getpid() & 0xFFFF) | (PACKET_FANOUT_LB << 16);
  CHKERR(setsockopt(fd, SOL_PACKET, PACKET_FANOUT, &fanout, sizeof(fanout)));
  return fd;
}

int main(int argc, char** argv) {
  printf("Removing old socket\n");
  CHKERR(unlink(kSocketName));
  printf("Creating socket\n");
  int sock = CHKERR(socket(AF_UNIX, SOCK_STREAM, 0));
  struct sockaddr_un addr;
  addr.sun_family = AF_UNIX;
  strncpy(addr.sun_path, kSocketName, UNIX_PATH_MAX);
  printf("Binding\n");
  CHKERR(bind(sock, (struct sockaddr*)&addr, sizeof(addr)));
  printf("Listening\n");
  CHKERR(listen(sock, 5));

  void* ring = NULL;
  printf("Getting AF_PACKET FD\n");
  int tp3fd = AFPacket("em1", &ring);

  while (true) {
    printf("Accepting... ");
    fflush(stdout);
    struct sockaddr_un caddr;
    socklen_t clen = sizeof(caddr);
    int cfd = CHKERR(accept(sock, (struct sockaddr*)&caddr, &clen));
    printf("%d\n", cfd);
    ancil_send_fd(cfd, tp3fd);
    printf("Closing %d\n", cfd);
    CHKERR(close(cfd));
  }

  return 0;
}
