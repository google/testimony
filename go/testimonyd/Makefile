GO=go

all: testimonyd

.PHONY: all clean

clean:
	rm -rf testimonyd

testimonyd: *.c *.h *.go
	$(GO) build -o testimonyd && \
	sudo setcap 'CAP_NET_RAW+ep CAP_NET_ADMIN+ep CAP_IPC_LOCK+ep' testimonyd
