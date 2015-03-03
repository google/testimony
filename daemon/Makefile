CPP=g++
CFLAGS=-std=c++0x -I../libancillary-libancillary
LDFLAGS=-L../libancillary-libancillary -lancillary

all: testimonyd

.PHONY: all clean

clean:
	rm -rf *.o testimonyd

%.o: %.cc
	$(CPP) $(CFLAGS) -c -o $@ $<

testimonyd: daemon.o
	$(CPP) -o testimonyd $^ $(LDFLAGS)
