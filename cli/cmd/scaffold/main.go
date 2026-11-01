// Command scaffold is the CLI adapter for the scaffold toolset.
//
//	scaffold <op> [args] [--json]     run an operation (see `scaffold help`)
//	scaffold mcp                      serve every operation over MCP (stdio)
//	scaffold serve [--addr host:port] serve every operation over HTTP
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/pjol/scaffold/cli/adapters/cliargs"
	"github.com/pjol/scaffold/cli/adapters/httpapi"
	"github.com/pjol/scaffold/cli/adapters/mcpserver"
	"github.com/pjol/scaffold/cli/core"
)

const version = "1.0.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "scaffold:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	var home string
	asJSON := false
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			asJSON = true
		case a == "--home" && i+1 < len(args):
			home = args[i+1]
			i++
		case strings.HasPrefix(a, "--home="):
			home = strings.TrimPrefix(a, "--home=")
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		return help(home, rest)
	}
	root, err := core.FindHome(home)
	if err != nil {
		return err
	}
	kit := &core.Kit{Home: root}

	switch rest[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "mcp":
		return mcpserver.ServeStdio(ctx, kit, version)
	case "serve":
		addr := "127.0.0.1:7420"
		for i := 1; i < len(rest); i++ {
			if rest[i] == "--addr" && i+1 < len(rest) {
				addr = rest[i+1]
			} else if v, ok := strings.CutPrefix(rest[i], "--addr="); ok {
				addr = v
			}
		}
		srv := &http.Server{Addr: addr, Handler: httpapi.Handler(kit), ReadHeaderTimeout: 5 * time.Second}
		go func() { <-ctx.Done(); _ = srv.Close() }()
		fmt.Fprintf(os.Stderr, "scaffold API on http://%s/v1/ops\n", addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			return err
		}
		return nil
	}

	op, ok := kit.Find(rest[0])
	if !ok {
		return fmt.Errorf("unknown command %q (run `scaffold help`)", rest[0])
	}
	raw, err := cliargs.Parse(op.InputType(), rest[1:])
	if err != nil {
		return fmt.Errorf("%s: %w\n\n%s", op.Name(), err, cliargs.Usage(op.InputType()))
	}
	out, err := op.Invoke(ctx, raw)
	if err != nil {
		return err
	}
	if t, ok := out.(core.Texter); ok && !asJSON {
		fmt.Print(t.Text())
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func help(home string, rest []string) error {
	kit := &core.Kit{}
	if root, err := core.FindHome(home); err == nil {
		kit.Home = root
	}
	if len(rest) > 1 {
		if op, ok := kit.Find(rest[1]); ok {
			fmt.Printf("scaffold %s — %s\n\n%s", op.Name(), op.Summary(), cliargs.Usage(op.InputType()))
			return nil
		}
	}
	fmt.Println("scaffold — concrete app foundation + feature-belt modules")
	fmt.Println()
	for _, op := range kit.Operations() {
		fmt.Printf("  %-8s %s\n", op.Name(), op.Summary())
	}
	fmt.Println()
	fmt.Println("  mcp      Serve every operation as an MCP tool over stdio.")
	fmt.Println("  serve    Serve every operation over HTTP (default 127.0.0.1:7420).")
	fmt.Println()
	fmt.Println("Global flags: --json (machine output), --home <dir> (scaffold home).")
	fmt.Println("`scaffold help <command>` shows a command's flags.")
	return nil
}
