CC=gcc
CFLAGS=-I../ancillary -Wall -g -O2
LDFLAGS=-L../ancillary -lancillary

all: libtestimony.a

.PHONY: all clean

clean:
	rm -rf *.o *.a *.so

%.o: %.c
	$(CC) $(CFLAGS) -c -o $@ $<

libtestimony.a: testimony.o
	ar cr $@ $^ && \
	ranlib $@
