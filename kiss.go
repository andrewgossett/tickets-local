package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	kissFEND  = byte(0xc0)
	kissFESC  = byte(0xdb)
	kissTFEND = byte(0xdc)
	kissTFESC = byte(0xdd)
)

func readKISSFrames(reader io.Reader, receive func([]byte)) error {
	buffered := bufio.NewReaderSize(reader, 4096)
	frame := make([]byte, 0, 512)
	inFrame := false
	escaped := false
	for {
		value, err := buffered.ReadByte()
		if err != nil {
			return err
		}
		if value == kissFEND {
			if inFrame && len(frame) > 1 && frame[0]&0x0f == 0 {
				decoded := append([]byte(nil), frame[1:]...)
				receive(decoded)
			}
			frame = frame[:0]
			inFrame = true
			escaped = false
			continue
		}
		if !inFrame {
			continue
		}
		if escaped {
			switch value {
			case kissTFEND:
				value = kissFEND
			case kissTFESC:
				value = kissFESC
			default:
				frame = frame[:0]
				inFrame = false
				escaped = false
				continue
			}
			escaped = false
		} else if value == kissFESC {
			escaped = true
			continue
		}
		if len(frame) >= 4096 {
			frame = frame[:0]
			inFrame = false
			escaped = false
			continue
		}
		frame = append(frame, value)
	}
}

func ax25UIToTNC2(frame []byte) (string, error) {
	if len(frame) < 16 {
		return "", errors.New("AX.25 frame is too short")
	}
	addresses := make([]ax25Address, 0, 4)
	offset := 0
	for {
		if len(addresses) >= 10 || offset+7 > len(frame) {
			return "", errors.New("AX.25 address field is malformed")
		}
		address, err := decodeAX25Address(frame[offset:offset+7], len(addresses) >= 2)
		if err != nil {
			return "", err
		}
		addresses = append(addresses, address)
		last := frame[offset+6]&0x01 != 0
		offset += 7
		if last {
			break
		}
	}
	if len(addresses) < 2 || offset+2 > len(frame) {
		return "", errors.New("AX.25 frame is missing source, control, or PID")
	}
	if frame[offset] != 0x03 || frame[offset+1] != 0xf0 {
		return "", errors.New("AX.25 frame is not an APRS UI frame")
	}
	information := frame[offset+2:]
	if len(information) == 0 || len(information) > 2048 {
		return "", errors.New("AX.25 information field is invalid")
	}
	for _, value := range information {
		if value == '\r' || value == '\n' || value == 0 {
			return "", errors.New("AX.25 information field contains invalid control characters")
		}
	}

	destination := addresses[0].String()
	source := addresses[1].String()
	path := make([]string, 0, len(addresses)-2)
	for _, address := range addresses[2:] {
		value := address.String()
		if address.repeated {
			value += "*"
		}
		path = append(path, value)
	}
	header := source + ">" + destination
	if len(path) > 0 {
		header += "," + strings.Join(path, ",")
	}
	return header + ":" + string(information), nil
}

type ax25Address struct {
	callsign string
	ssid     int
	repeated bool
}

func (address ax25Address) String() string {
	if address.ssid == 0 {
		return address.callsign
	}
	return fmt.Sprintf("%s-%d", address.callsign, address.ssid)
}

func decodeAX25Address(encoded []byte, digi bool) (ax25Address, error) {
	if len(encoded) != 7 {
		return ax25Address{}, errors.New("AX.25 address must contain seven bytes")
	}
	callsignBytes := make([]byte, 0, 6)
	for _, value := range encoded[:6] {
		character := value >> 1
		if character == ' ' {
			continue
		}
		if !((character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')) {
			return ax25Address{}, errors.New("AX.25 address contains an invalid callsign")
		}
		callsignBytes = append(callsignBytes, character)
	}
	if len(callsignBytes) == 0 {
		return ax25Address{}, errors.New("AX.25 address contains an empty callsign")
	}
	return ax25Address{
		callsign: string(callsignBytes),
		ssid:     int((encoded[6] >> 1) & 0x0f),
		repeated: digi && encoded[6]&0x80 != 0,
	}, nil
}
