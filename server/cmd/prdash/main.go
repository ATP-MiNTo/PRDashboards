// Command prdash serves the PR Dashboards UI and API on loopback.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
)

var version = "dev"

const (
	defaultPort    = 4317
	defaultDataDir = "~/.prdashboards"
)

func main() {
	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = run(ctx, opts, env{
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		open:    openBrowser,
		clock:   time.Now,
		rand:    rand.Reader,
		version: version,
		prompt:  secrets.TerminalPrompter(os.Stdin, os.Stderr),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "prdash:", err)
		os.Exit(1)
	}
}

type options struct {
	port      int
	dataDir   string
	noBrowser bool
	// fake starts in-process fake providers. The flag exists only in the e2e build.
	fake bool
}

// extraFlags registers build-specific flags. The e2e build adds --fake.
var extraFlags func(fs *flag.FlagSet, o *options)

func parseFlags(args []string, out io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("prdash", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.IntVar(&o.port, "port", defaultPort, "loopback port to listen on")
	fs.StringVar(&o.dataDir, "data-dir", defaultDataDir, "directory for the lock, token and data files")
	fs.BoolVar(&o.noBrowser, "no-browser", false, "print the launch URL to stderr instead of opening the browser")
	if extraFlags != nil {
		extraFlags(fs, &o)
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(out, "unexpected argument %q\n", fs.Arg(0))
		return o, errors.New("unexpected argument")
	}
	if o.port < 1 || o.port > 65535 {
		fmt.Fprintf(out, "invalid --port %d: must be between 1 and 65535\n", o.port)
		return o, errors.New("invalid port")
	}
	return o, nil
}
