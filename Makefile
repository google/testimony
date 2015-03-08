all: go c

clean: go_clean c_clean

.PHONY: c go c_clean go_clean all clean

go:
	$(MAKE) -C go

go_clean:
	$(MAKE) -C go clean

c:
	$(MAKE) -C c

c_clean:
	$(MAKE) -C c clean
