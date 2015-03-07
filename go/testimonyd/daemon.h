#ifndef __DAEMON_H__
#define __DAEMON_H__
int AFPacket(const char* iface, int block_size, int block_nr, int block_ms,
             int fanout_id, int fanout_type, int* fd, void** ring);
#endif
