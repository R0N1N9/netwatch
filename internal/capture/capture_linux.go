//go:build linux

// Package capture provides a pure-Go AF_PACKET layer-2 socket: no libpcap, no
// cgo. It can both inject raw frames (for ARP sweeps) and receive them (for the
// live monitor and ICE detectors).
package capture

import (
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// EtherType protocol selectors for OpenProto.
const (
	ProtoAll = 0x0003 // ETH_P_ALL — every frame
	ProtoARP = 0x0806 // ETH_P_ARP — ARP only
	ProtoIP  = 0x0800 // ETH_P_IP  — IPv4 only
)

// Handle is an open AF_PACKET socket bound to one interface.
type Handle struct {
	fd    int
	iface *net.Interface
}

func htons(h uint16) uint16 { return (h << 8) | (h >> 8) }

// Open binds to iface and receives all frames.
func Open(iface *net.Interface) (*Handle, error) { return OpenProto(iface, ProtoAll) }

// OpenProto binds to iface, receiving only frames of the given EtherType.
func OpenProto(iface *net.Interface, proto uint16) (*Handle, error) {
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(proto)))
	if err != nil {
		return nil, err
	}
	sll := &unix.SockaddrLinklayer{
		Protocol: htons(proto),
		Ifindex:  iface.Index,
	}
	if err := unix.Bind(fd, sll); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return &Handle{fd: fd, iface: iface}, nil
}

// Send injects a complete layer-2 frame.
func (h *Handle) Send(frame []byte) error {
	return unix.Sendto(h.fd, frame, 0, &unix.SockaddrLinklayer{Ifindex: h.iface.Index})
}

// Recv blocks until a frame arrives (or the read timeout elapses).
func (h *Handle) Recv() ([]byte, error) {
	buf := make([]byte, 65536)
	n, _, err := unix.Recvfrom(h.fd, buf, 0)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// SetReadTimeout bounds how long Recv blocks; a timeout surfaces as an error.
func (h *Handle) SetReadTimeout(d time.Duration) error {
	tv := unix.NsecToTimeval(d.Nanoseconds())
	return unix.SetsockoptTimeval(h.fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
}

// Interface returns the bound interface.
func (h *Handle) Interface() *net.Interface { return h.iface }

// Close releases the socket.
func (h *Handle) Close() error { return unix.Close(h.fd) }
