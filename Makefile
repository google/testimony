all: testimonyd c

clean: testimonyd_clean c_clean

.PHONY: c testimonyd

testimonyd:
	$(MAKE) -C testimonyd

testimonyd_clean:
	$(MAKE) -C testimonyd clean

c:
	$(MAKE) -C c

c_clean:
	$(MAKE) -C c clean
