package passive

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
)

// JA3 computes the JA3 fingerprint of a TLS ClientHello (the bytes starting at
// the TLS record header, 0x16...). Returns the JA3 string, its md5 hash, SNI,
// and ok=false if the buffer is not a parseable ClientHello.
func JA3(b []byte) (ja3 string, hash string, sni string, ok bool) {
	// record header (5) + handshake header (4) minimum
	if len(b) < 44 || b[0] != 0x16 || b[5] != 0x01 {
		return "", "", "", false
	}
	p := 9 // start of client_version inside the handshake
	if p+2 > len(b) {
		return "", "", "", false
	}
	version := binary.BigEndian.Uint16(b[p:])
	p += 2
	p += 32 // random
	if p >= len(b) {
		return "", "", "", false
	}
	// session id
	sidLen := int(b[p])
	p += 1 + sidLen
	// cipher suites
	if p+2 > len(b) {
		return "", "", "", false
	}
	csLen := int(binary.BigEndian.Uint16(b[p:]))
	p += 2
	if p+csLen > len(b) {
		return "", "", "", false
	}
	var ciphers []string
	for i := 0; i+1 < csLen; i += 2 {
		v := binary.BigEndian.Uint16(b[p+i:])
		if !grease(v) {
			ciphers = append(ciphers, strconv.Itoa(int(v)))
		}
	}
	p += csLen
	// compression methods
	if p >= len(b) {
		return "", "", "", false
	}
	cmLen := int(b[p])
	p += 1 + cmLen
	// extensions
	var exts, curves, formats []string
	if p+2 <= len(b) {
		extTot := int(binary.BigEndian.Uint16(b[p:]))
		p += 2
		end := p + extTot
		if end > len(b) {
			end = len(b)
		}
		for p+4 <= end {
			etype := binary.BigEndian.Uint16(b[p:])
			elen := int(binary.BigEndian.Uint16(b[p+2:]))
			p += 4
			if p+elen > end {
				break
			}
			data := b[p : p+elen]
			if !grease(etype) {
				exts = append(exts, strconv.Itoa(int(etype)))
			}
			switch etype {
			case 0x0000: // SNI
				sni = parseSNI(data)
			case 0x000a: // supported_groups
				curves = parseList16(data)
			case 0x000b: // ec_point_formats
				formats = parseList8(data)
			}
			p += elen
		}
	}

	ja3 = strconv.Itoa(int(version)) + "," +
		strings.Join(ciphers, "-") + "," +
		strings.Join(exts, "-") + "," +
		strings.Join(curves, "-") + "," +
		strings.Join(formats, "-")
	sum := md5.Sum([]byte(ja3))
	return ja3, hex.EncodeToString(sum[:]), sni, true
}

// IsClientHello reports whether a payload looks like a TLS ClientHello.
func IsClientHello(b []byte) bool {
	return len(b) > 5 && b[0] == 0x16 && b[5] == 0x01
}

func grease(v uint16) bool {
	return (v&0xff) == (v>>8) && (v&0x0f) == 0x0a
}

func parseList16(d []byte) []string {
	if len(d) < 2 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(d))
	d = d[2:]
	var out []string
	for i := 0; i+1 < n && i+1 < len(d); i += 2 {
		v := binary.BigEndian.Uint16(d[i:])
		if !grease(v) {
			out = append(out, strconv.Itoa(int(v)))
		}
	}
	return out
}

func parseList8(d []byte) []string {
	if len(d) < 1 {
		return nil
	}
	n := int(d[0])
	d = d[1:]
	var out []string
	for i := 0; i < n && i < len(d); i++ {
		out = append(out, strconv.Itoa(int(d[i])))
	}
	return out
}

func parseSNI(d []byte) string {
	// server_name_list(2) + type(1) + name_len(2) + name
	if len(d) < 5 {
		return ""
	}
	nameLen := int(binary.BigEndian.Uint16(d[3:]))
	if 5+nameLen > len(d) {
		return ""
	}
	return string(d[5 : 5+nameLen])
}
