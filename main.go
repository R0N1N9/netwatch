// netwatch — a cyberpunk network console.
//
// The Watchtower deck (defensive) watches your local network and guards the wall.
// Offensive (authorized recon/lab) and RUView (3D spatial) decks build on the
// same shell.
package main

import "github.com/r0n1n9/netwatch/internal/cli"

func main() {
	cli.Execute()
}
