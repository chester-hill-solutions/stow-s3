package mcpstorage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type objectKeyInput struct {
	Key string `json:"key"`
}
type objectSaveInput struct {
	Key              string            `json:"key"`
	DataBase64       string            `json:"data_base64"`
	ContentType      string            `json:"content_type,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	ObservationToken string            `json:"observation_token,omitempty"`
	Replace          bool              `json:"replace,omitempty"`
	RequestKey       string            `json:"request_key"`
}
type objectResolveInput struct {
	Key        string `json:"key"`
	RequestKey string `json:"request_key"`
}
type objectReleaseInput struct {
	ObservationToken string `json:"observation_token"`
}
type objectCapabilitiesInput struct{}

func (host *ObjectServer) addTools() {
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_object_read_for_save", Title: "Read an object for saving",
		Description: "Read one object's bytes and metadata from the configured bucket and return a short-lived comparison token. Use it before a guarded save, or to check what a key contains; do not use it to browse the bucket, which stow_browser pages by prefix. The token expires, so save promptly. Returns outcome observed, base64 bytes, ETag and observation_token; refusals carry a next step.",
		Annotations: readOnlyTool("Read an object for saving")}, host.readObject)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_object_save", Title: "Save an object",
		Description: "Save one object using the observation token from stow_object_read_for_save, or replace true to overwrite deliberately. Persist request_key before calling: retrying with the same key returns the original receipt instead of a second write, and that is the only safe retry. Do not reuse a key for a different body or path. Returns outcome committed, not_committed or unknown, metadata, and a next step on refusal.",
		Annotations: writeTool("Save an object", true)}, host.saveObject)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_object_resolve_save", Title: "Resolve an object save",
		Description: "Return the retained result of an earlier object save, identified by its original request key. Use it after a timeout, a dropped reply or outcome unknown; never use it to start a new save. It is safe to call repeatedly. Returns the same receipt with replayed true.",
		Annotations: readOnlyTool("Resolve an object save")}, host.resolveObject)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_object_release_observation", Title: "Release an object observation",
		Description: "Release a live object observation token and stop counting it against the observation limit. Use it when the edit will not happen; an unknown or expired token is an error. Saved receipts are unaffected.",
		Annotations: writeTool("Release an object observation", false)}, host.releaseObservation)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_object_capabilities", Title: "Inspect object capabilities",
		Description: "Report the configured object profile: allowed bucket, byte and object bounds, observation limits and whether saves are enabled. Use it when a call was refused for a bound you do not know. Read-only, and it does not disclose host paths.",
		Annotations: readOnlyTool("Inspect object capabilities")}, host.objectCapabilities)
	host.Server.AddReceivingMiddleware(host.limitCalls)
}

func (host *ObjectServer) readObject(ctx context.Context, _ *mcp.CallToolRequest, input objectKeyInput) (*mcp.CallToolResult, objectResult, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.beforeOperation(ctx); err != nil {
		return finishObject(objectResult{}, err)
	}
	if err := host.runtime.Authority().Check(stow.ObjectRead); err != nil {
		return finishObject(objectResult{}, err)
	}
	host.reapObservations()
	if len(host.observations) >= host.config.MaxObservations {
		return finishObject(objectResult{}, objectError{"observation_full"})
	}
	if err := host.readPreflight(ctx, input.Key); err != nil {
		return finishObject(objectResult{}, err)
	}
	object, condition, err := host.runtime.ReadForSave(ctx, host.config.Bucket, input.Key)
	if err != nil {
		return finishObject(objectResult{}, err)
	}
	if len(object.Data) > host.config.MaxObjectBytes {
		return finishObject(objectResult{}, objectError{"payload_too_large"})
	}
	token, err := host.retainObservation(input.Key, condition)
	if err != nil {
		return finishObject(objectResult{}, err)
	}
	value := objectResult{Outcome: "ok", Available: true, ObservationToken: token, Absent: condition.ObservedAbsence()}
	if !value.Absent {
		value.Object = objectMetadata(object, true)
	}
	response, value, err := finishObject(value, nil)
	if value.Error != nil || err != nil {
		delete(host.observations, token)
	}
	return response, value, err
}

func (host *ObjectServer) readPreflight(ctx context.Context, key string) error {
	if err := storage.ValidateKey(key); err != nil {
		return objectError{"invalid"}
	}
	object, err := host.runtime.HeadObject(ctx, host.config.Bucket, key)
	if errors.Is(err, stow.ErrObjectNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if object.Size > int64(host.config.MaxObjectBytes) {
		return objectError{"payload_too_large"}
	}
	response, _, err := finishObject(objectResult{Outcome: "ok", Object: objectMetadata(object, false)}, nil)
	if err != nil {
		return err
	}
	if response.IsError {
		return objectError{"response_too_large"}
	}
	return nil
}

func (host *ObjectServer) saveObject(ctx context.Context, _ *mcp.CallToolRequest, input objectSaveInput) (*mcp.CallToolResult, objectResult, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.beforeOperation(ctx); err != nil {
		return finishObject(objectResult{}, err)
	}
	if err := host.saveAuthority(); err != nil {
		return finishObject(objectResult{}, err)
	}
	data, condition, err := host.saveInput(input)
	if err != nil {
		return finishObject(objectResult{}, err)
	}
	saved, err := host.runtime.SaveObject(ctx, host.config.Bucket, input.Key, data, stow.SaveOptions{Condition: condition, RequestKey: input.RequestKey, PutOptions: stow.PutOptions{ContentType: input.ContentType, Metadata: input.Metadata}})
	return finishObject(savedObjectResult(saved), err)
}

func (host *ObjectServer) saveAuthority() error {
	granted := host.runtime.Authority()
	if err := granted.Check(stow.ObjectWrite); err != nil {
		return err
	}
	return granted.Check(stow.ObjectRead)
}

func (host *ObjectServer) saveInput(input objectSaveInput) ([]byte, stow.SaveCondition, error) {
	if input.RequestKey == "" || storage.ValidateKey(input.Key) != nil || (input.ObservationToken != "") == input.Replace {
		return nil, stow.SaveCondition{}, objectError{"invalid"}
	}
	encoded, _ := json.Marshal(input)
	if len(encoded) > maxResponseBytes || len(input.DataBase64) > base64.StdEncoding.EncodedLen(host.config.MaxObjectBytes) {
		return nil, stow.SaveCondition{}, objectError{"payload_too_large"}
	}
	data, err := base64.StdEncoding.Strict().DecodeString(input.DataBase64)
	if err != nil {
		return nil, stow.SaveCondition{}, objectError{"invalid"}
	}
	if len(data) > host.config.MaxObjectBytes {
		return nil, stow.SaveCondition{}, objectError{"payload_too_large"}
	}
	if input.Replace {
		return data, stow.ReplacementCondition(), nil
	}
	condition, err := host.observation(input.ObservationToken, input.Key)
	return data, condition, err
}

func savedObjectResult(saved stow.SaveResult) objectResult {
	value := objectResult{Outcome: string(saved.Outcome), Replayed: saved.Replayed}
	if saved.Outcome == stow.SaveCommitted {
		value.Object = objectMetadata(saved.Object, false)
		value.Available = true
	}
	return value
}

func (host *ObjectServer) resolveObject(ctx context.Context, _ *mcp.CallToolRequest, input objectResolveInput) (*mcp.CallToolResult, objectResult, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.beforeOperation(ctx); err != nil {
		return finishObject(objectResult{}, err)
	}
	host.reapObservations()
	if storage.ValidateKey(input.Key) != nil || input.RequestKey == "" {
		return finishObject(objectResult{}, objectError{"invalid"})
	}
	saved, err := host.runtime.ResolveSave(ctx, host.config.Bucket, input.Key, input.RequestKey)
	return finishObject(savedObjectResult(saved), err)
}

func (host *ObjectServer) releaseObservation(ctx context.Context, _ *mcp.CallToolRequest, input objectReleaseInput) (*mcp.CallToolResult, objectResult, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.beforeOperation(ctx); err != nil {
		return finishObject(objectResult{}, err)
	}
	host.reapObservations()
	if input.ObservationToken == "" {
		return finishObject(objectResult{}, objectError{"invalid"})
	}
	delete(host.observations, input.ObservationToken)
	return finishObject(objectResult{Outcome: "ok", Available: true, Released: true}, nil)
}

func (host *ObjectServer) objectCapabilities(ctx context.Context, _ *mcp.CallToolRequest, _ objectCapabilitiesInput) (*mcp.CallToolResult, objectResult, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.beforeOperation(ctx); err != nil {
		return finishObject(objectResult{}, err)
	}
	host.reapObservations()
	if err := host.runtime.Authority().Check(stow.ObjectRead); err != nil {
		return finishObject(objectResult{}, err)
	}
	caps := host.runtime.Capabilities()
	return finishObject(objectResult{Outcome: "ok", Available: true, Capabilities: &objectCapabilities{Backend: string(caps.Backend), Persistent: caps.Persistent, GuardedSaves: caps.GuardedSaves, DurableSaveRequests: caps.DurableSaveRequests, ReadOnly: host.config.ReadOnly, MaxObjectBytes: host.config.MaxObjectBytes, MaxObservations: host.config.MaxObservations, ObservationTTLSeconds: int(observationTTL.Seconds()), MaxResponseBytes: maxResponseBytes, MaxToolArgumentsBytes: maxResponseBytes, MaxConcurrentCalls: 8}}, nil)
}
