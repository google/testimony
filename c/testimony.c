#include <sys/socket.h>  // socket(), connect()
#include <sys/types.h>   // See socket man page
#include <sys/un.h>      // sockaddr_un
#include <errno.h>       // errno
#include <unistd.h>      // close()
#include <stdio.h>       // printf()
#include <sys/mman.h>    // mmap(), MAP_*, PROT_*

#include <testimony.h>

#ifdef __cplusplus
extern "C" {
#endif

// With much thanks to
// http://blog.varunajayasiri.com/passing-file-descriptors-between-processes-using-sendmsg-and-recvmsg
static int recv_file_descriptor(int socket) {
  struct msghdr message;
  struct iovec iov[1];
  struct cmsghdr* control_message = NULL;
  char ctrl_buf[CMSG_SPACE(sizeof(int))];
  char data[1];
  int res;

  memset(&message, 0, sizeof(struct msghdr));
  memset(ctrl_buf, 0, CMSG_SPACE(sizeof(int)));

  /* For the dummy data */
  iov[0].iov_base = data;
  iov[0].iov_len = sizeof(data);

  message.msg_name = NULL;
  message.msg_namelen = 0;
  message.msg_control = ctrl_buf;
  message.msg_controllen = CMSG_SPACE(sizeof(int));
  message.msg_iov = iov;
  message.msg_iovlen = 1;

  if ((res = recvmsg(socket, &message, 0)) <= 0) return res;

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

int testimony_init(testimony* t, const char* socket_name, const char* name) {
  struct sockaddr_un saddr;
  int r, err;
  memset(t, 0, sizeof(*t));
  saddr.sun_family = AF_UNIX;
  strncpy(saddr.sun_path, socket_name, sizeof(saddr.sun_path));
  saddr.sun_path[sizeof(saddr.sun_path) - 1] = 0;

  t->sock_fd = socket(AF_UNIX, SOCK_SEQPACKET, 0);
  if (t->sock_fd < 0) {
    goto fail;
  }
  r = connect(t->sock_fd, (struct sockaddr*)&saddr, sizeof(saddr));
  if (r < 0) {
    goto fail;
  }

  t->afpacket_fd = recv_file_descriptor(t->sock_fd);
  if (t->afpacket_fd < 0) {
    goto fail;
  }

  printf("Got AF_PACKET FD: %d\n", t->afpacket_fd);
  t->ring = mmap(NULL, 16 << 20, PROT_READ,
                 MAP_SHARED | MAP_LOCKED | MAP_NORESERVE, t->afpacket_fd, 0);
  if (t->ring == MAP_FAILED) {
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

#ifdef __cplusplus
}
#endif
