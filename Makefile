all: daemon c

clean: daemon_clean c_clean

.PHONY: c daemon

daemon:
	$(MAKE) -C daemon

daemon_clean:
	$(MAKE) -C daemon clean

c:
	$(MAKE) -C c

c_clean:
	$(MAKE) -C c clean
