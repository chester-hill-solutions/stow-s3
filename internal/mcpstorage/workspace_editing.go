package mcpstorage

import (
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func NewWorkspaceEditing(config Config, edit stow.WorkspaceEditOptions, recoverFileSaves bool) (*WorkspaceFiles, error) {
	if config.CheckpointID != "" || edit.Coordination != "host_quiesced" || len(edit.WriteRoots) == 0 {
		return nil, fmt.Errorf("workspace editing requires a mutable workspace, write roots, and host_quiesced coordination")
	}
	host, err := NewWorkspaceFiles(config, false)
	if err != nil {
		return nil, err
	}
	options := stow.WorkspaceOptions{RegistryDir: config.RegistryDir}
	var owner *stow.Workspace
	if recoverFileSaves {
		owner, err = stow.ResumeWorkspaceForFileRecovery(options, config.WorkspaceID)
	} else {
		owner, err = stow.ResumeWith(options, config.WorkspaceID)
	}
	if err != nil {
		_ = host.Close()
		return nil, fmt.Errorf("workspace editing unavailable")
	}
	host.owner = owner
	edit.ReadRoots = append([]string(nil), config.ReadRoots...)
	edit.WriteRoots = append([]string(nil), edit.WriteRoots...)
	editor, err := owner.OpenFileEditor(edit)
	if err != nil {
		_ = host.Close()
		return nil, fmt.Errorf("workspace editing unavailable")
	}
	host.editor = editor
	host.editing = true
	host.writeRoots = canonicalWorkspaceReadRoots(edit.WriteRoots)
	host.addWorkspaceEditingTools()
	host.Server.AddReceivingMiddleware(host.limitWorkspaceEditCalls)
	return host, nil
}
