package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

const bucket = "dogfood"

func main() {
	start := time.Now()
	root, err := os.MkdirTemp("", "stow-dogfood-")
	must(err)
	defer os.RemoveAll(root)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	input := []byte("synthetic input retained outside the store")
	must(os.WriteFile(filepath.Join(root, "input.txt"), input, 0600))
	steps := filesystemWorkflow(ctx, root, input)
	workspaceFailure(ctx, root)
	steps = append(steps, "workspace index failure, committed bytes, repair and reopen")
	must(json.NewEncoder(os.Stdout).Encode(struct {
		Steps    []string `json:"verified_paths"`
		Duration int64    `json:"duration_ms"`
	}{steps, time.Since(start).Milliseconds()}))
}

func openFilesystem(root string) *stow.Runtime {
	r, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: filepath.Join(root, "store"), Options: stow.Options{MaxBytes: 1 << 20, MaxObjects: 16}})
	must(err)
	return r
}

func filesystemWorkflow(ctx context.Context, root string, input []byte) []string {
	r := openFilesystem(root)
	defer r.Close()
	must(r.CreateBucket(ctx, bucket))
	_, err := r.PutObject(ctx, bucket, "inputs/input.txt", input, properties("input"))
	must(err)
	got, err := r.GetObject(ctx, bucket, "inputs/input.txt")
	must(err)
	verifyObject(got, string(input), "input")
	opts := initialSave(ctx, r)
	verifyReplay(ctx, r, opts)
	must(r.Close())
	r = openFilesystem(root)
	defer r.Close()
	verifyReopen(ctx, r, opts)
	pages := verifyPages(ctx, r)
	if pages != 2 {
		panic("two input/report objects must produce two pages")
	}
	steps := []string{"native SDK input put/get", "guarded save and stale guard refusal", "exact retry and changed-input refusal", "retry after newer write preserves current bytes", "close/reopen and retained receipt resolution", "new guard after reopen", "two one-entry list pages"}
	output, err := json.Marshal(steps)
	must(err)
	must(os.WriteFile(filepath.Join(root, "verification.json"), output, 0600))
	_, err = r.PutObject(ctx, bucket, "outputs/verification.json", output, properties("verification"))
	must(err)
	must(r.Close())
	r = openFilesystem(root)
	defer r.Close()
	got, err = r.GetObject(ctx, bucket, "outputs/verification.json")
	must(err)
	verifyObject(got, string(output), "verification")
	return append(steps, "verification output persisted in Stow and independently")
}

func initialSave(ctx context.Context, r *stow.Runtime) stow.SaveOptions {
	_, guard, err := r.ReadForSave(ctx, bucket, "outputs/report.txt")
	must(err)
	opts := stow.SaveOptions{Condition: guard, RequestKey: "dogfood-save-1", PutOptions: properties("original")}
	saved, err := r.SaveObject(ctx, bucket, "outputs/report.txt", []byte("original"), opts)
	must(err)
	if saved.Outcome != stow.SaveCommitted || saved.Replayed {
		panic("initial guarded save was not a fresh commit")
	}
	stale := opts
	stale.RequestKey = "stale-guard"
	refused, err := r.SaveObject(ctx, bucket, "outputs/report.txt", []byte("stale"), stale)
	if !errors.Is(err, stow.ErrSaveConflict) || refused.Outcome != stow.SaveNotCommitted {
		panic("stale guarded save was not refused")
	}
	return opts
}

func verifyReplay(ctx context.Context, r *stow.Runtime, opts stow.SaveOptions) {
	replayed, err := r.SaveObject(ctx, bucket, "outputs/report.txt", []byte("original"), opts)
	must(err)
	if !replayed.Replayed {
		panic("identical retry was not replayed")
	}
	verifyObject(replayed.Object, "", "original")
	_, err = r.SaveObject(ctx, bucket, "outputs/report.txt", []byte("different"), opts)
	if !errors.Is(err, stow.ErrSaveRequestConflict) {
		panic("changed input reused a retained request key")
	}
	_, err = r.PutObject(ctx, bucket, "outputs/report.txt", []byte("later"), properties("later"))
	must(err)
	replayed, err = r.SaveObject(ctx, bucket, "outputs/report.txt", []byte("original"), opts)
	must(err)
	if !replayed.Replayed || replayed.Object.Metadata["state"] != "original" {
		panic("retained result changed after a newer write")
	}
	got, err := r.GetObject(ctx, bucket, "outputs/report.txt")
	must(err)
	verifyObject(got, "later", "later")
}

func verifyReopen(ctx context.Context, r *stow.Runtime, old stow.SaveOptions) {
	got, err := r.GetObject(ctx, bucket, "outputs/report.txt")
	must(err)
	verifyObject(got, "later", "later")
	resolved, err := r.ResolveSave(ctx, bucket, "outputs/report.txt", old.RequestKey)
	must(err)
	if resolved.Outcome != stow.SaveCommitted || resolved.Object.Metadata["state"] != "original" {
		panic("reopen lost the original receipt")
	}
	_, err = r.SaveObject(ctx, bucket, "outputs/report.txt", []byte("original"), old)
	if !errors.Is(err, stow.ErrInvalidSaveCondition) {
		panic("a condition from the closed runtime remained usable")
	}
	_, guard, err := r.ReadForSave(ctx, bucket, "outputs/report.txt")
	must(err)
	saved, err := r.SaveObject(ctx, bucket, "outputs/report.txt", []byte("final"), stow.SaveOptions{Condition: guard, RequestKey: "dogfood-save-2", PutOptions: properties("final")})
	must(err)
	if saved.Outcome != stow.SaveCommitted {
		panic("new guard after reopen did not commit")
	}
	got, err = r.GetObject(ctx, bucket, "outputs/report.txt")
	must(err)
	verifyObject(got, "final", "final")
}

func verifyPages(ctx context.Context, r *stow.Runtime) int {
	opts := stow.ListOptions{Limit: 1}
	for pages := 1; pages <= 4; pages++ {
		page, err := r.ListObjects(ctx, bucket, opts)
		must(err)
		if len(page.Objects) != 1 {
			panic("list page did not contain one object")
		}
		if !page.Truncated {
			return pages
		}
		opts.Cursor = page.NextCursor
	}
	panic("bounded list did not terminate")
}

func workspaceFailure(ctx context.Context, root string) {
	opts := stow.WorkspaceOptions{Dir: filepath.Join(root, "workspace"), Bucket: bucket, RegistryDir: filepath.Join(root, "registry")}
	w, err := stow.OpenWorkspace(opts)
	must(err)
	defer w.Close()
	index := filepath.Join(opts.Dir, ".stow", "index.json")
	must(os.Rename(index, index+".saved"))
	must(os.Mkdir(index, 0700))
	result, err := w.PutObject(ctx, bucket, "report.txt", []byte("committed"), properties("committed"))
	if !errors.Is(err, stow.ErrMutationCommitted) || result.Metadata["state"] != "committed" || w.Usage().Bytes != 9 {
		panic("index failure hid its committed effect")
	}
	actual, err := os.ReadFile(filepath.Join(opts.Dir, "report.txt"))
	must(err)
	if !bytes.Equal(actual, []byte("committed")) {
		panic("reported commitment differs from host bytes")
	}
	must(os.Remove(index))
	must(os.Rename(index+".saved", index))
	must(w.Close())
	w, err = stow.OpenWorkspace(opts)
	must(err)
	defer w.Close()
	got, err := w.GetObject(ctx, bucket, "report.txt")
	must(err)
	verifyObject(got, "committed", "committed")
}

func properties(state string) stow.PutOptions {
	return stow.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": state}}
}

func verifyObject(object stow.Object, body, state string) {
	if string(object.Data) != body || object.Metadata["state"] != state || object.ContentType != "text/plain" || object.ETag == "" {
		panic("persisted bytes or metadata differ from the expected object")
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
