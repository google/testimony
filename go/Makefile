# Copyright 2015 Google Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

all: testimony testimonyd buildtestclient

test: testimony testimonyd testimony_test testimonyd_test testclient_test

clean: testimony_clean testimonyd_clean testclient_clean

install: testimonyd_install

.PHONY: testimony testimonyd

testimony:
	$(MAKE) -C testimony

testimony_test:
	$(MAKE) -C testimony test

testimony_clean:
	$(MAKE) -C testimony clean

testimonyd:
	$(MAKE) -C testimonyd

testimonyd_test:
	$(MAKE) -C testimonyd test

testimonyd_clean:
	$(MAKE) -C testimonyd clean

testimonyd_install:
	$(MAKE) -C testimonyd install

buildtestclient:
	$(MAKE) -C testclient

testclient_test:
	$(MAKE) -C testclient test

testclient_clean:
	$(MAKE) -C testclient clean
