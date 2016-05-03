#!/bin/bash

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

DUMMY="${DUMMY-dummy0}"
DIR="$(mktemp -d)"
SOCKET="$DIR/socket"
CONFIG="$DIR/config"
BASEDIR="${BASEDIR-/tmp}"

set -e
cd $(dirname $0)

function Log {
  FORMAT=$1
  shift
  echo -e "${FORMAT}$(date +%H:%M:%S.%N) --- $@\e[0m"
}
function Info {
  Log "\e[7m" "$@"
}
function Error {
  Log "\e[41m" "$@"
}
function Die {
  Error "$@"
  for file in `find $DIR -type f | sort`; do
    Info "$file"
    cat $file
  done
  exit 1
}
function Kill {
  sudo kill "$@" && sleep 1 && (sudo kill -9 "$@" || true)
}

Info "Testing sudo access"
sudo cat /dev/null
Info "Installing tcpreplay"
sudo apt-get install -y tcpreplay

Info "Building"
pushd ../
make
popd

cat > $CONFIG << EOF
[
  {
      "SocketName": "$SOCKET"
    , "Interface": "$DUMMY"
    , "BlockSize": 1048576
    , "NumBlocks": 16
    , "BlockTimeoutMillis": 1000
    , "FanoutSize": 1
    , "User": "$(whoami)"
    , "Filter": "host 169.254.1.1 and host 169.254.1.2"
  }
]
EOF

Info "Setting up $DUMMY interface"
sudo /sbin/modprobe dummy
sudo ip link add $DUMMY type dummy || Error "$DUMMY may already exist"
sudo ifconfig $DUMMY promisc up

Info "Starting testimony"
sudo ../go/testimonyd/testimonyd --config=$CONFIG --syslog=false 2>&1 &
DAEMON_PID="$!"
sleep 1
sudo kill -0 $DAEMON_PID || Die "Daemon not running"

Info "Starting clients"
CLIENT_PIDS=""
for i in {1..5}; do
  ../go/testclient/testclient --socket=$SOCKET --dump --count=10 >$DIR/out$i 2>$DIR/err$i &
  CLIENT_PIDS="$CLIENT_PIDS $!"
done
for i in {6..10}; do
  LD_LIBRARY_PATH=../c strace -e recvfrom,sendto -o $DIR/strace$i ../c/testimony_client --socket=$SOCKET --dump --count=10 >$DIR/out$i 2>$DIR/err$i &
  CLIENT_PIDS="$CLIENT_PIDS $!"
done
sleep 1
sudo kill -0 $CLIENT_PIDS || Die "Client not running"

Info "Sending packets to $DUMMY"
sudo tcpreplay -i $DUMMY --topspeed test.pcap
sleep 2

Info "Turning off client"
Kill $CLIENT_PIDS || Info "Failed to stop client, expected"
Info "Turning off daemon"
Kill $DAEMON_PID || Error "Failed to stop daemon"

Info "Testing client output"
for i in {1..10}; do
  diff -Naur $DIR/out$i test.expected || Die "Output from client $i failed, see $DIR/out$i and $DIR/err$i"
done
rm -rf $DIR
sudo ip link delete $DUMMY || Error "Failed to clean up dummy $DUMMY"
Info "SUCCESS"
