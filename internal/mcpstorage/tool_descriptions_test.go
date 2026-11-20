package mcpstorage

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func assertToolDescriptions(t *testing.T, session *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]*mcp.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
		if len(tool.Description) < 200 {
			t.Errorf("%s description is %d characters; the published bar is 3-4 sentences covering when and when not", tool.Name, len(tool.Description))
		}
		if sentences(tool.Description) < 3 {
			t.Errorf("%s description has %d sentences: %q", tool.Name, sentences(tool.Description), tool.Description)
		}
		if tool.Annotations == nil || tool.Annotations.Title == "" {
			t.Errorf("%s has no annotation title", tool.Name)
		}
		if tool.OutputSchema == nil {
			t.Errorf("%s has no output schema", tool.Name)
		}
	}
	return byName
}

func sentences(value string) int {
	count := strings.Count(value, ". ")
	if strings.HasSuffix(strings.TrimSpace(value), ".") {
		count++
	}
	return count
}

func TestWorkspaceEditingDescriptionsMeetThePublishedBar(t *testing.T) {
	_, session, _ := workspaceEditingFixture(t, []string{"docs"})
	tools := assertToolDescriptions(t, session)
	if tools["stow_workspace_file_read_for_save"].Annotations.ReadOnlyHint != true {
		t.Error("read_for_save is not marked read-only")
	}
	if tools["stow_workspace_file_save"].Annotations.ReadOnlyHint {
		t.Error("save is marked read-only")
	}
	if tools["stow_workspace_file_save"].Annotations.DestructiveHint == nil || !*tools["stow_workspace_file_save"].Annotations.DestructiveHint {
		t.Error("save does not disclose that it replaces content")
	}
}

func TestObjectToolDescriptionsMeetThePublishedBar(t *testing.T) {
	_, session := objectFixture(t, ObjectConfig{})
	tools := assertToolDescriptions(t, session)
	if tools["stow_object_read_for_save"].Annotations.ReadOnlyHint != true {
		t.Error("object read is not marked read-only")
	}
	if tools["stow_object_save"].Annotations.ReadOnlyHint {
		t.Error("object save is marked read-only")
	}
}
