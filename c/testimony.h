#ifndef __TESTIMONY_H__
#define __TESTIMONY_H__

#include <linux/if_packet.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
  int sock_fd;
  int afpacket_fd;
  char* ring;
  int block_size;
  int block_nr;
} testimony;

// Returns 0 on success, -errno on failure.
int testimony_init(testimony* t, const char* socket_name, const char* name, int num);
int testimony_close(testimony* t);
// Get a block of packets.
int testimony_get_block(testimony* t, struct tpacket_block_desc** block);
// Return a block of packets returned by testimony_get_block.
int testimony_return_block(testimony* t, struct tpacket_block_desc* block);


#ifdef __cplusplus
}
#endif

#endif
