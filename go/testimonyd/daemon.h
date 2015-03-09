#ifndef __DAEMON_H__
#define __DAEMON_H__
struct sock_fprog;
int AFPacket(const char* iface, int block_size, int block_nr, int block_ms,
             int fanout_id, int fanout_type, const struct sock_fprog* filt,
             // Outputs:
             int* fd, void** ring);
#endif
