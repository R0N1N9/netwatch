//go:build !linux

// Stub so the project still compiles (and the non-capture commands work) on
// non-Linux hosts. The AF_PACKET backend is Linux-only; macOS/Windows BPF and
// Npcap backends can be added here later behind the same Handle API.
package capture

import (
	"errors"
	"net"
	"time"
)

const (
	ProtoAll = 0x0003
	ProtoARP = 0x0806
	ProtoIP  = 0x0800
)

var errUnsupported = errors.New("capture: raw packet capture is only implemented on Linux (AF_PACKET) so far")

// Handle is a non-functional placeholder on non-Linux platforms.
type Handle struct{ iface *net.Interface }

func Open(iface *net.Interface) (*Handle, error) { return nil, errUnsupported }

func OpenProto(iface *net.Interface, proto uint16) (*Handle, error) { return nil, errUnsupported }

func (h *Handle) Send(frame []byte) error { return errUnsupported }

func (h *Handle) Recv() ([]byte, error) { return nil, errUnsupported }

func (h *Handle) SetReadTimeout(d time.Duration) error { return errUnsupported }

func (h *Handle) Interface() *net.Interface { return h.iface }

func (h *Handle) Close() error { return nil }
