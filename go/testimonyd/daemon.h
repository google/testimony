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

#ifndef __DAEMON_H__
#define __DAEMON_H__
struct sock_fprog;
int AFPacket(const char* iface, int block_size, int block_nr, int block_ms,
             int fanout_id, int fanout_type, const struct sock_fprog* filt,
             // Outputs:
             int* fd, void** ring, const char** err);
#endif
