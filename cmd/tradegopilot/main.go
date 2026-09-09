package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/protobuf/encoding/protojson"
	"tradegopilot/internal/invest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	config, err := invest.ConfigFromEnv()
	if err != nil {
		return err
	}
	client, err := invest.New(config)
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	response, md, err := client.Accounts(ctx)
	if ids := md.Get("x-tracking-id"); len(ids) > 0 {
		fmt.Fprintln(os.Stderr, "tracking-id:", ids[0])
	}
	if err != nil {
		return err
	}
	data, err := (protojson.MarshalOptions{Multiline: true, Indent: "  ", EmitUnpopulated: true}).Marshal(response)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}
