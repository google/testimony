CPP=g++
CFLAGS=-std=c++0x -Wall -fno-strict-aliasing
LDFLAGS=

all: testimonyd

.PHONY: all clean

clean:
	rm -rf *.o testimonyd

%.o: %.cc
	$(CPP) $(CFLAGS) -c -o $@ $<

testimonyd: daemon.o
	$(CPP) -o testimonyd $^ $(LDFLAGS) && \
	sudo setcap 'CAP_NET_RAW+ep CAP_NET_ADMIN+ep CAP_IPC_LOCK+ep' testimonyd
