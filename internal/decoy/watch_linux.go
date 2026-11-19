//go:build linux

package decoy

import (
	"time"

	"golang.org/x/sys/unix"
)

// watchFile calls onOpen whenever path is opened, until stop is called.
func watchFile(path string, onOpen func()) (stop func(), err error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return nil, err
	}
	if _, err := unix.InotifyAddWatch(fd, path, unix.IN_OPEN); err != nil {
		unix.Close(fd)
		return nil, err
	}
	armed := time.Now().Add(300 * time.Millisecond) // ignore our own write settling
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := unix.Read(fd, buf)
			if err != nil || n <= 0 {
				return
			}
			if time.Now().After(armed) {
				onOpen()
			}
		}
	}()
	return func() { unix.Close(fd) }, nil
}
