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

#include <sys/socket.h>  // socket(), connect()
#include <sys/types.h>   // See socket man page
#include <sys/un.h>      // sockaddr_un
#include <errno.h>       // errno
#include <unistd.h>      // close()
#include <stdio.h>       // printf()
#include <sys/mman.h>    // mmap(), MAP_*, PROT_*
#include <stdint.h>      // uint32_t
#include <poll.h>        // poll(), POLLIN, pollfd
#include <stdlib.h>      // malloc(), free()

#include <testimony.h>

#ifdef __cplusplus
extern "C" {
#endif

struct testimony_internal {
  int sock_fd;
  int afpacket_fd;
  unsigned char* ring;
  size_t block_size;
  size_t block_nr;
};

static const char kProtocolVersion = 1;

static uint32_t get_be_32(unsigned char* a) {
#define LSH(x, n) (((uint32_t)x) << n)
  return LSH(a[0], 24) | LSH(a[1], 16) | LSH(a[2], 8) | a[3];
#undef LSH
}
static void set_be_32(unsigned char* a, uint32_t x) {
  a[0] = x >> 24;
  a[1] = x >> 16;
  a[2] = x >> 8;
  a[3] = x;
}
static int recv_be_32(int fd, size_t* out) {
  unsigned char msg[4];
  unsigned char* writeto = msg;
  unsigned char* limit = msg + 4;
  int r;
  while (writeto < limit) {
    r = recv(fd, writeto, limit - writeto, 0);
    if (r < 0) {
      return -1;
    }
    writeto += r;
  }
  *out = get_be_32(msg);
  return 0;
}
static int send_be_32(int fd, size_t in) {
  unsigned char msg[4];
  unsigned char* readfrom = msg;
  unsigned char* limit = msg + 4;
  int r;
  set_be_32(msg, in);
  while (readfrom < limit) {
    r = send(fd, readfrom, limit - readfrom, 0);
    if (r < 0) {
      return -1;
    }
    readfrom += r;
  }
  return 0;
}

// With much thanks to
// http://blog.varunajayasiri.com/passing-file-descriptors-between-processes-using-sendmsg-and-recvmsg
static int recv_file_descriptor(int socket, size_t* block_size, size_t* block_nr) {
  struct msghdr message;
  struct iovec iov[1];
  struct cmsghdr* control_message = NULL;
  unsigned char ctrl_buf[CMSG_SPACE(sizeof(int))];
  unsigned char data[8];
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
  if (r != sizeof(data)) {
    return -1;
  }
  *block_size = get_be_32(data);
  *block_nr = get_be_32(data + 4);

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

int testimony_init(testimony* tp, const char* socket_name, int num) {
  struct sockaddr_un saddr, laddr;
  int r, err;
  unsigned char msg;
  testimony t = (testimony)malloc(sizeof(struct testimony_internal));
  if (t == NULL) {
    return -ENOMEM;
  }
  memset(t, 0, sizeof(*t));

  saddr.sun_family = AF_UNIX;
  strncpy(saddr.sun_path, socket_name, sizeof(saddr.sun_path) - 1);
  saddr.sun_path[sizeof(saddr.sun_path) - 1] = 0;

  t->sock_fd = socket(AF_UNIX, SOCK_STREAM, 0);
  if (t->sock_fd < 0) {
    // fprintf(stderr, "socket\n");
    goto fail;
  }
  // TODO(gconnell):  Don't use tmpnam here... figure out how to use mkstemp.
  laddr.sun_family = AF_UNIX;
  strcpy(laddr.sun_path, tmpnam(NULL));
  r = bind(t->sock_fd, (struct sockaddr*)&laddr, sizeof(laddr));
  if (r < 0) {
    // fprintf(stderr, "bind\n");
    goto fail;
  }

  r = connect(t->sock_fd, (struct sockaddr*)&saddr, sizeof(saddr));
  if (r < 0) {
    // fprintf(stderr, "connect\n");
    goto fail;
  }
  r = recv(t->sock_fd, &msg, sizeof(msg), 0);
  if (r < 0) {
    // fprintf(stderr, "recv\n");
    goto fail;
  } else if (msg != kProtocolVersion) {
    // fprintf(stderr, "version\n");
    errno = EPROTONOSUPPORT;
    goto fail;
  }
  msg = num;
  r = send(t->sock_fd, &msg, 1, 0);
  if (r < 0) {
    // fprintf(stderr, "send\n");
    goto fail;
  }

  t->afpacket_fd =
      recv_file_descriptor(t->sock_fd, &t->block_size, &t->block_nr);
  if (t->afpacket_fd < 0) {
    // fprintf(stderr, "recv_file_descriptor\n");
    goto fail;
  }

  printf("Got AF_PACKET FD: %d (%d/%d)\n", t->afpacket_fd, t->block_size,
         t->block_nr);
  t->ring = mmap(NULL, t->block_size * t->block_nr, PROT_READ,
                 MAP_SHARED | MAP_LOCKED | MAP_NORESERVE, t->afpacket_fd, 0);
  if (t->ring == MAP_FAILED) {
    t->ring = 0;
    // fprintf(stderr, "mmap\n");
    errno = EINVAL;
    goto fail;
  }
  printf("Got ring: %p\n", t->ring);
  *tp = t;
  return 0;

fail:
  err = errno;
  testimony_close(t);
  return -err;
}

int testimony_close(testimony t) {
  if (t->ring != 0) {
    if (munmap(t->ring, t->block_nr * t->block_size) < 0) return -errno;
  }
  if (close(t->sock_fd) < 0) return -errno;
  free(t);
  return 0;
}

int testimony_get_block(testimony t, int timeout_millis,
                        struct tpacket_block_desc** block) {
  struct pollfd pfd;
  size_t blockidx;
  int r;
  *block = NULL;
  if (t->sock_fd == 0 || t->ring == 0) {
    return -EINVAL;
  }
  if (timeout_millis >= 0) {
    memset(&pfd, 0, sizeof(pfd));
    pfd.fd = t->sock_fd;
    pfd.events = POLLIN;
    r = poll(&pfd, 1, timeout_millis);
    if (r < 0) {
      return -errno;
    }
    if (r == 0) {
      return 0;  // Timed out, no block ready yet.
    }
    // A read is ready, fall through.
  }
  r = recv_be_32(t->sock_fd, &blockidx);
  if (r < 0) {
    return -errno;
  }
  if (blockidx >= t->block_nr) {
    // fprintf(stderr, "%d >= %d\n", blockidx,t->block_nr);
    return -EIO;
  }
  *block =
      (struct tpacket_block_desc*)(t->ring + t->block_size * blockidx);
  return 0;
}

int testimony_return_block(testimony t, struct tpacket_block_desc* block) {
  int r;
  size_t blockptr = (size_t)block;
  blockptr -= (size_t)t->ring;
  blockptr /= t->block_size;
  if (blockptr >= t->block_nr) {
    return -EINVAL;
  }
  r = send_be_32(t->sock_fd, blockptr);
  if (r < 0) {
    return -errno;
  }
  return 0;
}

int testimony_block_size(testimony t) {
  if (t->sock_fd == 0) {
    return -1;
  }
  return t->block_size;
}

int testimony_block_nr(testimony t) {
  if (t->sock_fd == 0) {
    return -1;
  }
  return t->block_nr;
}

struct testimony_iter_internal {
  struct tpacket_block_desc* block;
  unsigned char* pkt;
  int left;
};

int testimony_iter_init(testimony_iter* iter) {
  *iter = (testimony_iter)malloc(sizeof(struct testimony_iter_internal));
  if (*iter == NULL) {
    return -ENOMEM;
  }
  memset(iter, 0, sizeof(*iter));
  return 0;
}

int testimony_iter_reset(
    testimony_iter iter, struct tpacket_block_desc* block) {
  if (block->version != 3) {
    return -EPROTONOSUPPORT;
  }
  iter->block = block;
  iter->left = block->hdr.bh1.num_pkts;
  iter->pkt = NULL;
  return 0;
}

struct tpacket3_hdr* testimony_iter_next(testimony_iter iter) {
  if (!iter->left) {
    return NULL;
  }
  iter->left--;
  if (iter->pkt) {
    iter->pkt += ((struct tpacket3_hdr*)iter->pkt)->tp_next_offset;
  } else {
    iter->pkt = (unsigned char*)iter->block
        + iter->block->hdr.bh1.offset_to_first_pkt;
  }
  return (struct tpacket3_hdr*)iter->pkt;
}

int testimony_iter_close(testimony_iter iter) {
  free(iter);
  return 0;
}

unsigned char* testimony_packet_data(struct tpacket3_hdr* pkt) {
  return (unsigned char*)pkt + pkt->tp_mac;
}
int64_t testimony_packet_nanos(struct tpacket3_hdr* pkt) {
  return (int64_t)pkt->tp_sec * 1000000000LL + pkt->tp_nsec;
}

#ifdef __cplusplus
}
#endif
