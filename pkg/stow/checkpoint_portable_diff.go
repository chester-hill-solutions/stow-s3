package stow

import (
	"reflect"
	"sort"
)

type CheckpointBucketChange struct {
	Bucket string `json:"bucket"`
	Kind   string `json:"kind"`
}
type CheckpointObjectChange struct {
	Bucket string            `json:"bucket"`
	Key    string            `json:"key"`
	Kind   string            `json:"kind"`
	From   *CheckpointObject `json:"from,omitempty"`
	To     *CheckpointObject `json:"to,omitempty"`
}
type PortableCheckpointDiff struct {
	Version int                      `json:"version"`
	Files   []CheckpointChange       `json:"files"`
	Buckets []CheckpointBucketChange `json:"buckets"`
	Objects []CheckpointObjectChange `json:"objects"`
}

func ComparePortableCheckpoints(registryDir, fromID, toID string) (PortableCheckpointDiff, error) {
	from, err := LoadCheckpoint(registryDir, fromID)
	if err != nil {
		return PortableCheckpointDiff{}, err
	}
	to, err := LoadCheckpoint(registryDir, toID)
	if err != nil {
		return PortableCheckpointDiff{}, err
	}
	return PortableCheckpointDiff{Version: 1, Files: diffCheckpointFiles(from.Files, to.Files), Buckets: diffPortableBuckets(from.Buckets, to.Buckets), Objects: diffPortableObjects(from.Objects, to.Objects)}, nil
}
func diffPortableBuckets(from, to []string) []CheckpointBucketChange {
	before, after := map[string]bool{}, map[string]bool{}
	for _, name := range from {
		before[name] = true
	}
	for _, name := range to {
		after[name] = true
	}
	var changes []CheckpointBucketChange
	for name := range before {
		if !after[name] {
			changes = append(changes, CheckpointBucketChange{Bucket: name, Kind: "deleted"})
		}
	}
	for name := range after {
		if !before[name] {
			changes = append(changes, CheckpointBucketChange{Bucket: name, Kind: "added"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Bucket < changes[j].Bucket })
	return changes
}
func diffPortableObjects(from, to []CheckpointObject) []CheckpointObjectChange {
	before, after := map[string]CheckpointObject{}, map[string]CheckpointObject{}
	keys := map[string]bool{}
	for _, object := range from {
		key := object.Bucket + "\x00" + object.Key
		before[key] = object
		keys[key] = true
	}
	for _, object := range to {
		key := object.Bucket + "\x00" + object.Key
		after[key] = object
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	var changes []CheckpointObjectChange
	for _, key := range ordered {
		old, hadOld := before[key]
		current, hasCurrent := after[key]
		if hadOld && hasCurrent && reflect.DeepEqual(old, current) {
			continue
		}
		change := CheckpointObjectChange{Bucket: current.Bucket, Key: current.Key, Kind: "changed"}
		if hadOld {
			change.From = &old
		}
		if hasCurrent {
			change.To = &current
		}
		if !hadOld {
			change.Kind = "added"
		}
		if !hasCurrent {
			change.Kind = "deleted"
			change.Bucket = old.Bucket
			change.Key = old.Key
		}
		changes = append(changes, change)
	}
	return changes
}
