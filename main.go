package main

import (
	"fmt"
	"os"

	"github.com/dantech2000/logx/cmd"
	"github.com/dantech2000/logx/internal/terminal"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", terminal.Sanitize(err.Error()))
		os.Exit(1)
	}
}
