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

all: go c

clean: go_clean c_clean

install: go_install c_install

.PHONY: c go

go:
	$(MAKE) -C go

go_clean:
	$(MAKE) -C go clean

go_install:
	$(MAKE) -C go install

c:
	$(MAKE) -C c

c_clean:
	$(MAKE) -C c clean

c_install:
	$(MAKE) -C c install
