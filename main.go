/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"context"
	"os"

	"github.com/appcd-dev/genie/cmd"
)

func main() {
	err := cmd.NewRootCommand().ExecuteContext(context.Background())
	if err != nil {
		os.Exit(1)
	}
}
