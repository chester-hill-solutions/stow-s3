package main

import (
	"context"
	"errors"
	"flag"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func destroyWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace destroy", flag.ContinueOnError)
	id := flags.String("id", "", "Workspace ID to destroy")
	registry := flags.String("registry-dir", "", "Workspace registry directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("workspace destroy requires --id")
	}
	ws, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: *registry}, *id)
	if err != nil {
		return err
	}
	defer ws.Close()
	if err := ws.Destroy(context.Background()); err != nil {
		return err
	}
	return writeWorkspaceJSON(struct {
		Version   int    `json:"version"`
		ID        string `json:"workspace_id"`
		Destroyed bool   `json:"destroyed"`
	}{1, *id, true})
}

func collectWorkspacesCommand(args []string) error {
	flags := flag.NewFlagSet("workspace collect", flag.ContinueOnError)
	registry := flags.String("registry-dir", "", "Workspace registry directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	results, err := stow.Collect(stow.CollectOptions{RegistryDir: *registry})
	if err != nil {
		return err
	}
	return writeWorkspaceJSON(struct {
		Version int                  `json:"version"`
		Results []stow.CollectResult `json:"results"`
	}{1, results})
}
