all: daemon c

clean: daemon_clean c_clean
	rm -rf libancillary-libancillary ancillary

.PHONY: c daemon

ancillary: libancillary-libancillary
	mv libancillary-libancillary ancillary
	$(MAKE) -C ancillary

libancillary-libancillary:
	curl -L https://gitorious.org/libancillary/libancillary/archive-tarball/master | tar xvz

daemon: ancillary
	$(MAKE) -C daemon

daemon_clean:
	$(MAKE) -C daemon clean

c:
	$(MAKE) -C c

c_clean:
	$(MAKE) -C c clean
