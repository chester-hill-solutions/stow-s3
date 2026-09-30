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
	if err := stow.DestroyRegisteredWorkspace(context.Background(), *registry, *id); err != nil {
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
// It reports the quiet ones first and marks the ones whose directory is gone,
// because those are the two things a list is for.
//
// Every entry is reported, including the ones that are not readable: an entry
// whose directory has been deleted is what a crashed or hand-cleaned run leaves
// behind, and it is the single thing a list is most worth finding. A flag to ask
// for it would mean the default output does not have it, and a caller who does
// not know the flag does not know the entry exists. Readable is on every summary,
// so dropping the broken ones is one line in whatever language the caller is
// written in.
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

// pruneWorkspacesCommand removes registry entries whose workspace directory is gone.
//
// It is a separate verb from collect rather than a flag on it, because the two ask
// different questions and only one of them is safe to run without thinking. Collect
// destroys directories and asks whether a workspace is finished with — a judgement
// about time and ownership. Prune asks whether there is anything there at all, which
// is a fact, and it removes stow's records and nothing else.
func pruneWorkspacesCommand(args []string) error {
	flags := flag.NewFlagSet("workspace prune", flag.ContinueOnError)
	registry := flags.String("registry-dir", "", "Workspace registry directory")
	team := flags.String("team", "", "Team partition within the registry directory")
	adopted := flags.Bool("include-adopted", false, "Also forget adopted entries whose directory is gone")
	if err := flags.Parse(args); err != nil {
		return err
	}
	results, err := stow.Prune(stow.PruneOptions{
		RegistryDir: *registry, Team: *team, IncludeAdopted: *adopted,
	})
	if err != nil {
		return err
	}
	removed := 0
	for _, result := range results {
		if result.Removed {
			removed++
		}
	}
	return writeWorkspaceJSON(struct {
		Version int                `json:"version"`
		Count   int                `json:"count"`
		Removed int                `json:"removed"`
		Results []stow.PruneResult `json:"results"`
	}{1, len(results), removed, results})
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
