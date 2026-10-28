// The check command validates the catalog directory and distributions.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/toshicloud/toshi-catalog/internal/catalog"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", ".", "catalog directory")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "check: flags: %v\n", err)
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "check: flags: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 1
	}
	c, problems, err := catalog.Load(*dir)
	for _, problem := range problems {
		fmt.Fprintln(stderr, problem.String())
	}
	if err != nil {
		fmt.Fprintf(stderr, "check: %v\n", err)
		return 1
	}
	if len(problems) > 0 {
		return 1
	}
	fmt.Fprintf(stdout, "catalog: %d publishers, %d products, %d distributions: ok\n", len(c.Publishers), len(c.Products), len(c.Distributions))
	return 0
}
