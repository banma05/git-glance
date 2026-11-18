// Command gitglance is the core of Git Glance. Editor shells run
// `gitglance rpc` and talk to it over standard input and output.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/rpc"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/web"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes one command and returns the process exit code. For rpc it
// returns only after ServeLines has returned.
func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: gitglance version | rpc | serve")
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, version)
		return 0
	case "rpc":
		return runRPC(ctx, in, out, errOut)
	case "serve":
		return runServe(ctx, args[1:], out, errOut)
	}
	fmt.Fprintf(errOut, "gitglance: unknown command %q\n", args[0])
	return 2
}

func runRPC(ctx context.Context, in io.Reader, out, errOut io.Writer) int {
	home, err := settings.Home()
	if err != nil {
		fmt.Fprintln(errOut, "gitglance:", err)
		return 1
	}
	h, err := rpc.NewHandler(gitx.ExecRunner{}, settings.Store{Dir: home})
	if err != nil {
		fmt.Fprintln(errOut, "gitglance:", err)
		return 1
	}
	h.SetCoreVersion(version)
	if err := rpc.ServeLines(ctx, h, in, out); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(errOut, "gitglance:", err)
		return 1
	}
	return 0
}

// runServe serves the browser mode until ctx is done and returns only after
// the web server has stopped.
func runServe(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(errOut)
	port := fs.Int("port", 0, "port on 127.0.0.1 (0 lets the system choose)")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *port < 0 || *port > 65535 || fs.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: gitglance serve [--port N] [--no-open]")
		return 2
	}
	home, err := settings.Home()
	if err != nil {
		fmt.Fprintln(errOut, "gitglance:", err)
		return 1
	}
	h, err := rpc.NewHandler(gitx.ExecRunner{}, settings.Store{Dir: home})
	if err != nil {
		fmt.Fprintln(errOut, "gitglance:", err)
		return 1
	}
	h.SetCoreVersion(version)
	ready := func(url string) { fmt.Fprintln(out, url) }
	if err := web.Serve(ctx, *port, h, !*noOpen, ready); err != nil {
		fmt.Fprintln(errOut, "gitglance:", err)
		return 1
	}
	return 0
}
