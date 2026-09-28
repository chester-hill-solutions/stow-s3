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
// the ids. It reports the quiet ones first and flags the ones whose directory is
// gone, because those are the two things a list is for.
func listWorkspacesCommand(args []string) error {
	flags := flag.NewFlagSet("workspace list", flag.ContinueOnError)
	registry := flags.String("registry-dir", "", "Workspace registry directory")
	team := flags.String("team", "", "Team partition within the registry directory")
	all := flags.Bool("all", false, "Include workspaces whose directory is missing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	summaries, err := stow.List(stow.ListWorkspacesOptions{RegistryDir: *registry, Team: *team})
	if err != nil {
		return err
	}
	results := make([]stow.WorkspaceSummary, 0, len(summaries))
	for _, summary := range summaries {
		// A missing directory is reported by default rather than hidden. It is the
		// state a crashed or hand-cleaned run leaves behind, and it is exactly what
		// somebody listing workspaces is looking for — so --all is the opt-in that
		// *includes* the broken ones, because the default that matters is the one
		// that surfaces a problem rather than the one that tidies it away.
		if !summary.Readable && !*all {
			continue
		}
		results = append(results, summary)
	}
	return writeWorkspaceJSON(struct {
		Version  int                     `json:"version"`
		Registry string                  `json:"registry_dir"`
		Team     string                  `json:"team,omitempty"`
		Count    int                     `json:"count"`
		Results  []stow.WorkspaceSummary `json:"results"`
	}{1, registryDirOrEmpty(*registry, *team), *team, len(results), results})
}

func registryDirOrEmpty(dir, team string) string {
	resolved, err := stow.ResolveRegistryDir(dir, team)
	if err != nil {
		return dir
	}
	return resolved
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
