package main

import (
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/OrionG-hub/laowangbot/plugins/monitor"
	"os"
	"path/filepath"
)

func main() {
	host, err := api.NewHost()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := api.StateDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	plugin, err := monitor.New(host, filepath.Join(dir, "monitor.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = api.Serve(plugin.Handle); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
