package rtsp

import (
	"encoding/binary"
	"fmt"
	"io"
)

// After PLAY is sent, the server starts pushing RTP packets over the same TCP connection using interleaved framing (RFC 2326 §10.12). It looks like this on the wire:$ 00 00 9C <RTP data...>
// │  │  └──┘
// │  │  length (2 bytes, big-endian)
// │  channel (1 byte, 0 = video)
// magic byte (always 0x24 = '$')So to read one RTP packet you:

// Read exactly 4 bytes (the interleaved header)
// Verify first byte is 0x24 ($)
// Extract length from bytes [2] and [3] (big-endian uint16)
// Read exactly length bytes (the RTP payload)
// Verify the payload is non-empty — that's your pixel data proof

// RTP packet structure (just enough to validate)
// The RTP payload itself starts with a 12-byte fixed header:
// byte 0: version (top 2 bits should be 0b10 = version 2)
// byte 1: payload type
// bytes 2-3: sequence number
// bytes 4-7: timestamp
// bytes 8-11: SSRC
// bytes 12+: actual media data
// You don't need to decode any of this — just verify:

// Payload length > 12 (has more than just the header)
// Top 2 bits of byte 0 are 10 (valid RTP version 2)

type RTPPacket struct {
	Channel uint8
	Length  uint16
	Payload []byte
}

func ReadRTPPacket(r io.Reader) (*RTPPacket, error) {
	// 1. read 4 bytes into header [4]byte
	header := make([]byte, 4)
	// 2. check header[0] == 0x24, error if not
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read RTP header: %w", err)
	}
	if header[0] != 0x24 {
		return nil, fmt.Errorf("invalid RTP magic byte: got %02x, expected $", header[0])
	}
	// 3. channel = header[1]
	channel := header[1]
	// 4. length = binary.BigEndian.Uint16(header[2:4])
	length := binary.BigEndian.Uint16(header[2:4])
	// 5. read `length` bytes as payload using io.ReadFull
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("failed to read RTP payload: %w", err)
	}
	// 6. validate: len(payload) > 12 && payload[0]>>6 == 2
	if len(payload) <= 12 {
		return nil, fmt.Errorf("invalid RTP payload: too short to contain header")
	}
	if payload[0]>>6 != 2 {
		return nil, fmt.Errorf("invalid RTP version: expected 2, got %d", payload[0]>>6)
	}
	// 7. return &RTPPacket{}
	return &RTPPacket{
		Channel: channel,
		Length:  length,
		Payload: payload,
	}, nil
}
