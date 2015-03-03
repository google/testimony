all: daemon

.PHONY: daemon clean all ancillary

ancillary: libancillary-libancillary
	$(MAKE) -C libancillary-libancillary

libancillary-libancillary:
	curl -L https://gitorious.org/libancillary/libancillary/archive-tarball/master | tar xvz

daemon: ancillary
	$(MAKE) -C daemon

daemon_clean:
	$(MAKE) -C daemon clean

clean: daemon_clean
	rm -rf libancillary-libancillary
