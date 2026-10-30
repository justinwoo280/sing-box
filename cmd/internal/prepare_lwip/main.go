package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sagernet/sing-box/cmd/internal/build_shared"
)

func main() {
	directory := flag.String("dir", "", "directory for prepared sources and the alternate modfile")
	flag.Parse()
	if *directory == "" {
		fmt.Fprintln(os.Stderr, "-dir is required")
		os.Exit(1)
	}
	modfile, err := build_shared.PrepareLWIPSource(*directory)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(modfile)
}
