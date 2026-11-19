//go:build !linux

package decoy

import "errors"

func watchFile(path string, onOpen func()) (func(), error) {
	return nil, errors.New("canary files need Linux inotify")
}
