// OpenCode Portable — a self-contained, USB-local OpenCode environment.
//
// The launcher detects the host, selects a compatible OpenCode runtime from
// the official distribution, prepares a USB-local environment and launches
// OpenCode with all arguments forwarded. Nothing is installed on the host.
package main

import (
	"os"

	"opencode-portable/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
