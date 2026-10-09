package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/modelprofiles"
)

func runProvidersCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) || args[0] != "image-model" {
		fmt.Fprintln(stderr, "Usage: mewla providers image-model [show | set <model> | clear] [--connection <id>]")
		return flag.ErrHelp
	}
	return runProvidersImageModel(args[1:], stderr)
}

// runProvidersImageModel shows or changes the image_model of Codex
// connections: the model the gateway sends for Codex's built-in image_gen
// instead of the gpt-image-2 Codex names.
func runProvidersImageModel(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla providers image-model", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	var connectionID string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.StringVar(&connectionID, "connection", "", "Codex connection id (default: the selected Codex connection)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla providers image-model [show | set <model> | clear] [--connection <id>]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Codex's built-in image_gen always asks for gpt-image-2. A Codex connection's")
		fmt.Fprintln(stderr, "image_model makes the gateway send that model instead on /v1/images/generations")
		fmt.Fprintln(stderr, "and /v1/images/edits; clear restores the unchanged request. The next image_gen")
		fmt.Fprintln(stderr, "call uses it; Codex does not restart.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	action := "show"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	req := control.Request{Type: "provider_list"}
	switch action {
	case "show":
		if len(positional) != 0 {
			return fmt.Errorf("usage: mewla providers image-model show")
		}
	case "set":
		if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
			return fmt.Errorf("usage: mewla providers image-model set <model> [--connection <id>]")
		}
		req = control.Request{Type: "provider_set_image_model", ConnectionID: connectionID, ModelID: positional[0]}
	case "clear":
		if len(positional) != 0 {
			return fmt.Errorf("usage: mewla providers image-model clear [--connection <id>]")
		}
		req = control.Request{Type: "provider_set_image_model", ConnectionID: connectionID}
	default:
		fs.Usage()
		return fmt.Errorf("unknown image-model action: %s", action)
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	if !resp.OK || resp.Providers == nil {
		return writeControlResponse(os.Stdout, resp, true)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(codexImageModels(*resp.Providers))
}

// parseInterleaved parses flags given before or after positional arguments
// (flag.Parse alone stops at the first positional one).
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	positional := []string{}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

type codexImageModelRow struct {
	ConnectionID string `json:"connection_id"`
	Name         string `json:"name"`
	Selected     bool   `json:"selected"`
	ImageModel   string `json:"image_model"`
}

func codexImageModels(proj modelprofiles.ProviderCatalogProjection) []codexImageModelRow {
	selected := proj.Defaults[modelprofiles.ClientCodex].ConnectionID
	rows := []codexImageModelRow{}
	for _, conn := range proj.Connections {
		isCodex := false
		for _, client := range conn.Clients {
			isCodex = isCodex || client == modelprofiles.ClientCodex
		}
		if isCodex {
			rows = append(rows, codexImageModelRow{ConnectionID: conn.ID, Name: conn.Name, Selected: conn.ID == selected, ImageModel: conn.ImageModel})
		}
	}
	return rows
}
