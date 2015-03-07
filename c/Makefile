AR=ar
RANLIB=ranlib
CC=gcc
CFLAGS=-fPIC -Wall -g -O2 -I. -fno-strict-aliasing
LDFLAGS=

all: libtestimony.a testimony_client

.PHONY: all clean

clean:
	rm -rf *.o *.a *.so *.so.* testimony_client

%.o: %.c
	$(CC) $(CFLAGS) -c -o $@ $<

testimony_client: testimony_client.o libtestimony.a
	$(CC) -o testimony_client testimony_client.o $(LDFLAGS) -L. -ltestimony

libtestimony.a: testimony.o
	$(AR) cr $@ $^ && \
	$(RANLIB) $@
