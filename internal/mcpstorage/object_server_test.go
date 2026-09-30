package mcpstorage

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func objectFixture(t *testing.T, config ObjectConfig) (*ObjectServer, *mcp.ClientSession) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("owned filesystem profile requires Darwin or Linux")
	}
	if config.Dir == "" {
		config.Dir = filepath.Join(t.TempDir(), "objects")
	}
	if config.Bucket == "" {
		config.Bucket = "objects"
		config.CreateBucket = true
	}
	host, err := NewObjects(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host, objectSession(t, host)
}

func objectSession(t *testing.T, host *ObjectServer) *mcp.ClientSession {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := host.Server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "stow-object-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func invokeObject(t *testing.T, session *mcp.ClientSession, name string, input interface{}) objectResult {
	t.Helper()
	args, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(response)
	if err != nil || len(wire) > maxResponseBytes {
		t.Fatalf("response bytes=%d, err=%v", len(wire), err)
	}
	data, err := json.Marshal(response.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var value objectResult
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if value.Version != 1 || response.IsError != (value.Error != nil) {
		t.Fatalf("invalid object result=%s", data)
	}
	var textValue objectResult
	text, ok := response.Content[0].(*mcp.TextContent)
	if !ok || json.Unmarshal([]byte(text.Text), &textValue) != nil || !reflect.DeepEqual(value, textValue) {
		t.Fatal("text and structured object output differ")
	}
	return value
}

func readObjectForTest(t *testing.T, session *mcp.ClientSession, key string) objectResult {
	t.Helper()
	return invokeObject(t, session, "stow_object_read_for_save", objectKeyInput{Key: key})
}
func saveObjectForTest(t *testing.T, session *mcp.ClientSession, input objectSaveInput) objectResult {
	t.Helper()
	return invokeObject(t, session, "stow_object_save", input)
}
func saveInputForTest(key, body, token, request string) objectSaveInput {
	return objectSaveInput{Key: key, DataBase64: base64.StdEncoding.EncodeToString([]byte(body)), ObservationToken: token, RequestKey: request}
}
func requireObjectCommitted(t *testing.T, result objectResult) {
	t.Helper()
	if result.Error != nil || result.Outcome != "committed" || !result.Available || result.Object == nil {
		t.Fatalf("save=%+v", result)
	}
}

func TestObjectToolSchemasAndCapabilities(t *testing.T) {
	_, session := objectFixture(t, ObjectConfig{})
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil || len(listed.Tools) != 5 {
		t.Fatalf("tools=%+v,%v", listed, err)
	}
	for _, tool := range listed.Tools {
		response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: json.RawMessage(`{"unexpected":true}`)})
		if err == nil && !response.IsError {
			t.Fatalf("%s accepted unknown input", tool.Name)
		}
	}
	for _, extra := range []string{"bucket", "path", "if_match"} {
		args, _ := json.Marshal(map[string]string{"key": "new", "data_base64": "", "observation_token": "fake", "request_key": "request", extra: "untrusted"})
		response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "stow_object_save", Arguments: json.RawMessage(args)})
		if err == nil && !response.IsError {
			t.Fatalf("accepted unsafe field %s", extra)
		}
	}
	result := invokeObject(t, session, "stow_object_capabilities", objectCapabilitiesInput{})
	if result.Error != nil || result.Capabilities == nil || !result.Capabilities.DurableSaveRequests || result.Capabilities.MaxObjectBytes != maxObjectPayload {
		t.Fatalf("caps=%+v", result)
	}
}
