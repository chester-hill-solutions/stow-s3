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

// listWorkspacesCommand enumerates the workspaces this machine knows about.
//
// Fifteen verbs and none of them listed, so a feature whose whole promise is "your
// work will still be here" could not be shown to anybody who had not kept a note of
// the ids. It reports the quiet ones first and marks the ones whose directory is
// gone, because those are the two things a list is for.
//
// Every entry is reported, including the ones that are not readable. There used to
// be an --all flag that included them, and it was the wrong way round: an entry
// whose directory has been deleted is the state a crashed or hand-cleaned run
// leaves behind, and it is the single thing a list is most worth finding. A flag to
// ask for it means the default output does not have it, and a caller who does not
// know the flag does not know the entry exists. Readable is on every summary, so
// dropping the broken ones is one line in whatever language the caller is written
// in — and the flag is gone from the CLI and from both wrappers because there is
// nothing left for it to select.
func listWorkspacesCommand(args []string) error {
	flags := flag.NewFlagSet("workspace list", flag.ContinueOnError)
	registry := flags.String("registry-dir", "", "Workspace registry directory")
	team := flags.String("team", "", "Team partition within the registry directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	listing, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: *registry, Team: *team})
	if err != nil {
		return err
	}
	return writeWorkspaceJSON(struct {
		Version  int                     `json:"version"`
		Registry string                  `json:"registry_dir"`
		Team     string                  `json:"team,omitempty"`
		Count    int                     `json:"count"`
		Results  []stow.WorkspaceSummary `json:"results"`
	}{1, listing.Registry, listing.Team, len(listing.Workspaces), listing.Workspaces})
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
