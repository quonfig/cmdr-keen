// Command cc-deck-hook is the standalone build of keen's Claude Code hook
// helper. It exists for anyone wiring hooks by hand; the normal path is keen
// invoking itself via the hidden `keen __hook` subcommand, so a single
// `go install` of keen is fully self-contained. Both share internal/hook.
package main

import (
	"os"

	"github.com/quonfig/cmdr-keen/internal/hook"
)

func main() {
	hook.Run(os.Args[1:])
}
