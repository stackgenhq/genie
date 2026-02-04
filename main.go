/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/appcd-dev/genie/cmd"
)

func main() {
	err := cmd.NewRootCommand().ExecuteContext(context.Background())
	if err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
}
