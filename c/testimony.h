#ifndef __TESTIMONY_H__
#define __TESTIMONY_H__

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
  int sock_fd;
  int afpacket_fd;
  void* ring;
} testimony;

int testimony_init(testimony* t, const char* socket_name, const char* name);

#ifdef __cplusplus
}
#endif

#endif
