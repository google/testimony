# Copyright 2015 Google Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

AR=ar
RANLIB=ranlib
CC=gcc
CFLAGS=-fPIC -Wall -g -O2 -I. -fno-strict-aliasing
LDFLAGS=

all: libtestimony.a libtestimony.so testimony_client

.PHONY: all clean install

clean:
	rm -rf *.o *.a *.so *.so.* testimony_client

%.o: %.c
	$(CC) $(CFLAGS) -c -o $@ $<

testimony_client: testimony_client.o libtestimony.a
	$(CC) -o testimony_client testimony_client.o $(LDFLAGS) -L. -ltestimony

libtestimony.a: testimony.o
	$(AR) cr $@ $^ && \
	$(RANLIB) $@

libtestimony.so: testimony.o
	$(CC) -shared -o $@ $^

install: libtestimony.a libtestimony.so
	cp -f -v libtestimony.{a,so} /usr/lib && \
	chmod 644 /usr/lib/libtestimony.{a,so} && \
	cp -f -v testimony.h /usr/include && \
	chmod 644 /usr/include/testimony.h
