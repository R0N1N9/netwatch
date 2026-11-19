// Package passive is netrunner's passive intelligence engine: live packet
// capture feeding a flow table and a set of analyzers (OS fingerprint, TLS/JA3,
// DNS/DGA, beaconing) plus a behavioral anomaly model. All observation only.
package passive

// OSGuess is a passive OS fingerprint derived from a TCP SYN.
type OSGuess struct {
	OS         string
	Confidence int // 0-100
}

// FingerprintSYN classifies the sender OS from SYN-packet characteristics
// (p0f-style): initial-TTL bucket refined by advertised window size.
func FingerprintSYN(ttl uint8, window, mss uint16) OSGuess {
	initTTL := 64
	switch {
	case ttl > 128:
		initTTL = 255
	case ttl > 64:
		initTTL = 128
	}

	switch initTTL {
	case 255:
		return OSGuess{"Network device / Solaris", 60}
	case 128:
		switch window {
		case 8192, 64240, 65535:
			return OSGuess{"Windows", 72}
		default:
			return OSGuess{"Windows", 60}
		}
	default: // 64
		switch window {
		case 65535:
			return OSGuess{"macOS / iOS / BSD", 58}
		case 29200, 14600, 5840, 64240, 42340:
			return OSGuess{"Linux / Android", 66}
		default:
			if mss == 1440 || mss == 1400 {
				return OSGuess{"Linux (tunneled)", 52}
			}
			return OSGuess{"Linux / Unix", 50}
		}
	}
}
