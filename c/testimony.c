#include <sys/socket.h>  // socket(), connect()
#include <sys/types.h>   // See socket man page
#include <sys/un.h>      // sockaddr_un
#include <errno.h>       // errno
#include <unistd.h>      // close()
#include <stdio.h>       // printf()
#include <sys/mman.h>    // mmap(), MAP_*, PROT_*
#include <stdint.h>      // uint32_t

#include <testimony.h>

#ifdef __cplusplus
extern "C" {
#endif

#define LSHIFT(x, n) (((uint32_t)(x)) << n)
static uint32_t get_be_uint32(char* buf) {
  return 0 | LSHIFT(buf[0], 24) | LSHIFT(buf[1], 16) | LSHIFT(buf[2], 8) |
         LSHIFT(buf[3], 0);
}
#undef LSHIFT
static void put_be_uint32(char* buf, uint32_t n) {
  buf[0] = n >> 24;
  buf[1] = n >> 16;
  buf[2] = n >> 8;
  buf[3] = n;
}
static int recv_be_uint32(int fd, int* n) {
  char buf[4];
  int received = 0;
  int r;
  while (received < 4) {
    r = recv(fd, buf + received, 4 - received, 0);
    if (r < 0) return -1;
    received += r;
  }
  *n = get_be_uint32(buf);
  return 0;
}
static int send_be_uint32(int fd, int n) {
  char buf[4];
  int r;
  int sent = 0;
  put_be_uint32(buf, n);
  while (sent < 4) {
    r = send(fd, buf + sent, 4 - sent, 0);
    if (r < 0) return -1;
    sent += r;
  }
  return 0;
}

// With much thanks to
// http://blog.varunajayasiri.com/passing-file-descriptors-between-processes-using-sendmsg-and-recvmsg
static int recv_file_descriptor(int socket, int* block_size, int* block_nr) {
  struct msghdr message;
  struct iovec iov[1];
  struct cmsghdr* control_message = NULL;
  char ctrl_buf[CMSG_SPACE(sizeof(int))];
  char data[8];
  int r;

  memset(&message, 0, sizeof(struct msghdr));
  memset(ctrl_buf, 0, CMSG_SPACE(sizeof(int)));

  /* For the block data */
  iov[0].iov_base = data;
  iov[0].iov_len = sizeof(data);

  message.msg_name = NULL;
  message.msg_namelen = 0;
  message.msg_control = ctrl_buf;
  message.msg_controllen = CMSG_SPACE(sizeof(int));
  message.msg_iov = iov;
  message.msg_iovlen = 1;

  r = recvmsg(socket, &message, 0);
  if (r != 8) {
    fprintf(stderr, "got %d, want 8\n", r);
    return -1;
  }
  *block_size = get_be_uint32(message.msg_iov[0].iov_base);
  *block_nr = get_be_uint32(message.msg_iov[0].iov_base + 4);

  /* Iterate through header to find if there is a file descriptor */
  for (control_message = CMSG_FIRSTHDR(&message); control_message != NULL;
       control_message = CMSG_NXTHDR(&message, control_message)) {
    if ((control_message->cmsg_level == SOL_SOCKET) &&
        (control_message->cmsg_type == SCM_RIGHTS)) {
      return *((int*)CMSG_DATA(control_message));
    }
  }

  return -1;
}

int testimony_init(testimony* t, const char* socket_name, const char* name,
                   int num) {
  struct sockaddr_un saddr, laddr;
  int n, r, err;
  char buf[1024];
  memset(t, 0, sizeof(*t));
  saddr.sun_family = AF_UNIX;
  strncpy(saddr.sun_path, socket_name, sizeof(saddr.sun_path) - 1);
  saddr.sun_path[sizeof(saddr.sun_path) - 1] = 0;

  t->sock_fd = socket(AF_UNIX, SOCK_STREAM, 0);
  if (t->sock_fd < 0) {
    fprintf(stderr, "socket\n");
    goto fail;
  }
  // TODO(gconnell):  Don't use tmpnam here... figure out how to use mkstemp.
  laddr.sun_family = AF_UNIX;
  strcpy(laddr.sun_path, tmpnam(NULL));
  r = bind(t->sock_fd, (struct sockaddr*)&laddr, sizeof(laddr));
  if (r < 0) {
    fprintf(stderr, "bind\n");
    goto fail;
  }

  r = connect(t->sock_fd, (struct sockaddr*)&saddr, sizeof(saddr));
  if (r < 0) {
    fprintf(stderr, "connect\n");
    goto fail;
  }
  n = snprintf(buf, 1024, "{\"Name\": \"%s\", \"Num\": %d}", name, num);
  if (n < 0 || n > 1024) {
    fprintf(stderr, "snprintf\n");
    goto fail;
  }
  r = send(t->sock_fd, buf, n, 0);
  if (r < 0) {
    fprintf(stderr, "send\n");
    goto fail;
  }

  t->afpacket_fd =
      recv_file_descriptor(t->sock_fd, &t->block_size, &t->block_nr);
  if (t->afpacket_fd < 0) {
    fprintf(stderr, "recv_file_descriptor\n");
    goto fail;
  }

  printf("Got AF_PACKET FD: %d (%d/%d)\n", t->afpacket_fd, t->block_size,
         t->block_nr);
  t->ring = mmap(NULL, t->block_size * t->block_nr, PROT_READ,
                 MAP_SHARED | MAP_LOCKED | MAP_NORESERVE, t->afpacket_fd, 0);
  if (t->ring == MAP_FAILED) {
    fprintf(stderr, "mmap\n");
    errno = EINVAL;
    goto fail;
  }
  printf("Got ring: %p\n", t->ring);
  return 0;

fail:
  err = errno;
  close(t->sock_fd);
  t->sock_fd = 0;
  t->afpacket_fd = 0;
  t->ring = 0;
  return -err;
}

int testimony_close(testimony* t) {
  if (t->ring != 0) {
    munmap(t->ring, t->block_nr * t->block_size);
  }
  return close(t->sock_fd);
}

int testimony_get_block(testimony* t, struct tpacket_block_desc** block) {
  int blockidx, r;
  if (t->sock_fd == 0 || t->ring == 0) {
    return -EINVAL;
  }
  r = recv_be_uint32(t->sock_fd, &blockidx);
  if (r < 0 || blockidx < 0 || blockidx >= t->block_nr) {
    return -EIO;
  }
  *block = (struct tpacket_block_desc*)(t->ring + t->block_size * blockidx);
  return 0;
}

int testimony_return_block(testimony* t, struct tpacket_block_desc* block) {
  int r;
  uintptr_t blockptr = (uintptr_t)block;
  blockptr -= (uintptr_t)t->ring;
  blockptr /= t->block_size;
  if (blockptr >= t->block_nr) {
    return -EINVAL;
  }
  r = send_be_uint32(t->sock_fd, blockptr);
  if (r < 0) {
    return -errno;
  } else if (r != 4) {
    return -EIO;
  }
  return 0;
}

#ifdef __cplusplus
}
#endif
