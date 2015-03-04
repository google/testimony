AR=ar
RANLIB=ranlib
CC=gcc
CFLAGS=-fPIC -Wall -g -O2 -I. -fno-strict-aliasing
LDFLAGS=

all: libtestimony.a

.PHONY: all clean

clean:
	rm -rf *.o *.a *.so *.so.*

%.o: %.c
	$(CC) $(CFLAGS) -c -o $@ $<

test: testimony_test

testimony_test: testimony_test.o libtestimony.a
	$(CC) -o testimony_test testimony_test.o $(LDFLAGS) -L. -ltestimony

libtestimony.a: testimony.o
	$(AR) cr $@ $^ && \
	$(RANLIB) $@
