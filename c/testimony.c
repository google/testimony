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
  char* ring;
  int block_size;
  int block_nr;
};

static const char kProtocolVersion = 1;

// With much thanks to
// http://blog.varunajayasiri.com/passing-file-descriptors-between-processes-using-sendmsg-and-recvmsg
static int recv_file_descriptor(int socket, int* block_size, int* block_nr) {
  struct msghdr message;
  struct iovec iov[1];
  struct cmsghdr* control_message = NULL;
  char ctrl_buf[CMSG_SPACE(sizeof(int))];
  char data[2];
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
    fprintf(stderr, "got %d, want %d\n", r, (int)sizeof(data));
    return -1;
  }
  *block_size = 1 << data[0];
  *block_nr = data[1];

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
  char msg;
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
  r = recv(t->sock_fd, &msg, 1, 0);
  if (r < 0) {
    fprintf(stderr, "recv\n");
    goto fail;
  } else if (msg != kProtocolVersion) {
    fprintf(stderr, "version\n");
    errno = EPROTONOSUPPORT;
    goto fail;
  }
  msg = num;
  r = send(t->sock_fd, &msg, 1, 0);
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
    t->ring = 0;
    fprintf(stderr, "mmap\n");
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

int testimony_get_block(testimony t, int timeout_millis, struct tpacket_block_desc** block) {
  struct pollfd pfd;
  char blockidx;
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
    if (r < 0) { return -errno; }  // error, return negative errno.
    if (r == 0) { return 0; }  // timed out, no block ready
    // A read is ready, fall through.
  }
  r = recv(t->sock_fd, &blockidx, 1, 0);
  if (r < 0) { return -errno; }
  if (r != 1 || blockidx < 0 || blockidx >= t->block_nr) {
    return -EIO;
  }
  *block =
      (struct tpacket_block_desc*)(t->ring + t->block_size * (int)blockidx);
  return 0;
}

int testimony_return_block(testimony t, struct tpacket_block_desc* block) {
  int r;
  uintptr_t blockptr = (uintptr_t)block;
  char blockidx;
  blockptr -= (uintptr_t)t->ring;
  blockptr /= t->block_size;
  if (blockptr >= t->block_nr) {
    return -EINVAL;
  }
  blockidx = blockptr;
  r = send(t->sock_fd, &blockidx, 1, 0);
  if (r != 1) {
    return -errno;
  }
  return 0;
}

int testimony_block_size(testimony t) {
  if (t->sock_fd == 0) { return -1; }
  return t->block_size;
}

int testimony_block_nr(testimony t) {
  if (t->sock_fd == 0) { return -1; }
  return t->block_nr;
}

#ifdef __cplusplus
}
#endif
