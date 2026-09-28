package main

import (
	"context"
	"fmt"
	api "github.com/OrionG-hub/laowangbot/pkg/pluginapi"
	"github.com/OrionG-hub/laowangbot/plugins/qdsg"
	"os"
)

func main() {
	dir, e := api.StateDir()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	host, e := api.NewHost()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	p, e := qdsg.New(context.Background(), host, dir)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	defer p.Close()
	if e = api.Serve(p.Handle); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
