package mcpstorage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type inspectInput struct {
	CheckpointID string `json:"checkpoint_id"`
	Limit        int    `json:"limit,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
}

type diffInput struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type cursor struct {
	Identity string `json:"identity"`
	Offset   int    `json:"offset"`
}

func (a *adapter) inspectCheckpoint(_ context.Context, _ *mcp.CallToolRequest, input inspectInput) (*mcp.CallToolResult, result, error) {
	cp, err := a.checkpoint(input.CheckpointID)
	if err != nil {
		return finish(result{}, err)
	}
	entries := make([]entry, 0, len(cp.Files)+len(cp.Excluded))
	for _, file := range cp.Files {
		f := file
		entries = append(entries, entry{Kind: "file", File: &f})
	}
	for _, object := range cp.Objects {
		o := object
		entries = append(entries, entry{Kind: "object", Object: &o})
	}
	for _, bucket := range cp.Buckets {
		entries = append(entries, entry{Kind: "bucket", Bucket: bucket})
	}
	for _, path := range cp.Excluded {
		entries = append(entries, entry{Kind: "excluded", Excluded: path})
	}
	page, next, err := a.page(entries, input.CheckpointID, input.Cursor, input.Limit)
	return finish(result{Outcome: "ok", CheckpointID: cp.ID, Entries: page, NextCursor: next}, err)
}

func (a *adapter) diff(_ context.Context, _ *mcp.CallToolRequest, input diffInput) (*mcp.CallToolResult, result, error) {
	if _, err := a.checkpoint(input.FromID); err != nil {
		return finish(result{}, err)
	}
	if _, err := a.checkpoint(input.ToID); err != nil {
		return finish(result{}, err)
	}
	changes, err := stow.ComparePortableCheckpoints(a.config.RegistryDir, input.FromID, input.ToID)
	if err != nil {
		return finish(result{}, err)
	}
	entries := make([]entry, 0, len(changes.Files)+len(changes.Objects)+len(changes.Buckets))
	for _, change := range changes.Files {
		c := change
		entries = append(entries, entry{Kind: "change", Change: &c})
	}
	for _, change := range changes.Objects {
		c := change
		entries = append(entries, entry{Kind: "object_change", ObjectChange: &c})
	}
	for _, change := range changes.Buckets {
		c := change
		entries = append(entries, entry{Kind: "bucket_change", BucketChange: &c})
	}
	page, next, err := a.page(entries, input.FromID+":"+input.ToID, input.Cursor, input.Limit)
	return finish(result{Outcome: "ok", Entries: page, NextCursor: next}, err)
}

func (a *adapter) page(entries []entry, identity, token string, limit int) ([]entry, string, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return nil, "", fmt.Errorf("page limit must be between 1 and 1000")
	}
	digest := sha256.Sum256([]byte(a.config.RegistryDir + "\x00" + a.config.WorkspaceID + "\x00" + identity))
	expected := hex.EncodeToString(digest[:])
	offset, err := cursorOffset(token, expected, len(entries))
	if err != nil {
		return nil, "", err
	}
	end := min(offset+limit, len(entries))
	next := ""
	if end < len(entries) {
		data, err := json.Marshal(cursor{Identity: expected, Offset: end})
		if err != nil {
			return nil, "", err
		}
		next = base64.RawURLEncoding.EncodeToString(data)
	}
	return entries[offset:end], next, nil
}

func cursorOffset(token, identity string, total int) (int, error) {
	if token == "" {
		return 0, nil
	}
	if len(token) > 256 {
		return 0, fmt.Errorf("invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	var parsed cursor
	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	if parsed.Identity != identity || parsed.Offset < 0 || parsed.Offset > total {
		return 0, fmt.Errorf("cursor does not match this result")
	}
	return parsed.Offset, nil
}
