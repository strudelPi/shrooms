package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/vpavlin/shrooms/internal/memstat"
)

// cmdMemory prints where the daemon's memory goes.
//
// The daemon runs as root, so its /proc entries are closed to the user asking;
// it reports this about itself instead (memstat). The split that matters is Go's
// share against the rest, which in shrooms is mostly the native delivery
// library — asked for to decide whether shrooms could fit an iOS packet tunnel
// extension's 50 MiB (docs/shrooms-on-ios.md).
func cmdMemory(args []string) error {
	fs := flag.NewFlagSet("memory", flag.ExitOnError)
	sock := fs.String("socket", DefaultSocket, "control socket path")
	if err := fs.Parse(splitArgs(fs, args)); err != nil {
		return err
	}
	st, err := fetchStatus(*sock)
	if err != nil {
		return err
	}
	if st.Memory == nil {
		return errors.New("this daemon does not report its memory; it predates `shrooms memory`")
	}
	fmt.Print(memstat.Format(*st.Memory))
	return nil
}
