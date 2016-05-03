// Copyright 2015 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Types used when sending requests/data between client and server.
type Type uint16

// Types used for sending data between client and server.
const (
	TypeBlockIndex Type = 0

	TypeServerToClient Type = 0x8000 + iota

	TypeWaitingForFanoutIndex
	TypeFanoutSize
	TypeBlockSize
	TypeNumBlocks

	TypeClientToServer Type = 0xC000 + iota

	TypeFanoutIndex

	TypeError Type = 0xFFFF
)

// TypeNames allows for printing of protocols.
var TypeNames = map[Type]string{
	TypeBlockIndex:            "BlockIndex",
	TypeServerToClient:        "ServerToClient",
	TypeWaitingForFanoutIndex: "WaitingForFanoutIndex",
	TypeFanoutSize:            "FanoutSize",
	TypeBlockSize:             "BlockSize",
	TypeNumBlocks:             "NumBlocks",
	TypeClientToServer:        "ClientToServer",
	TypeFanoutIndex:           "FanoutIndex",
	TypeError:                 "Error",
}

// TypeOf returns the high-level type (TypeServerToClient, TypeClientToServer,
// TypeBlockIndex, or TypeError) of a type.
func TypeOf(t Type) Type {
	switch {
	case t&0x8000 == 0:
		return TypeBlockIndex
	case t > TypeServerToClient && t < TypeClientToServer:
		return TypeServerToClient
	case t > TypeClientToServer && t < TypeError:
		return TypeClientToServer
	}
	return TypeError
}

// SendType sends a given type with a nil value to the given writer.
func SendType(to io.Writer, typ Type) error {
	return SendTLV(to, typ, nil)
}

// SendUint32 sends a given type with a uint32 value to the given writer.
func SendUint32(to io.Writer, typ Type, val uint32) error {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], val)
	return SendTLV(to, typ, buf[:])
}

// SendTLV sends an arbitrary-length value with the given type type to a writer.
func SendTLV(to io.Writer, typ Type, val []byte) error {
	if TypeOf(typ) != TypeServerToClient && TypeOf(typ) != TypeClientToServer {
		return fmt.Errorf("invalid send type %d", typ)
	} else if len(val) > 0xFFFF {
		return fmt.Errorf("too-long value (%d > %d)", len(val), 0xFFFF)
	}
	var buf [64]byte
	binary.BigEndian.PutUint32(buf[:], ToTL(typ, len(val)))
	if _, err := to.Write(append(buf[:4], val...)); err != nil {
		return fmt.Errorf("writing %d (len %d): %v", typ, len(val), err)
	}
	return nil
}

// TLFrom splits a uint32 into a type and length.
func TLFrom(from uint32) (typ Type, length int) {
	if from&0x80000000 == 0 {
		return TypeBlockIndex, 0
	}
	return Type(from >> 16), int(from & 0xFFFF)
}

// ToTL converts a type/length to a TL uint32.
func ToTL(typ Type, length int) uint32 {
	return (uint32(typ) << 16) + uint32(length)
}
