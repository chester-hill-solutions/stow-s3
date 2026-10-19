package runthrough

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough/outboxpb"
	"google.golang.org/protobuf/proto"
)

const outboxProtoMagic = "STOWOUT4"
const outboxProtoPrefixSize = len(outboxProtoMagic) + 4
const outboxProtoVersion = 4

func encodeOutboxProto(state outboxState) ([]byte, error) {
	message := &outboxpb.Outbox{Version: outboxProtoVersion, Seq: state.seq, NextToken: state.nextToken, Provenance: make([]*outboxpb.Provenance, 0, len(state.provenance))}
	keys := make([]string, 0, len(state.provenance))
	for key := range state.provenance {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if len(keys) != 0 {
		values := make([]outboxpb.Provenance, len(keys))
		for index, key := range keys {
			value := state.provenance[key]
			values[index] = outboxpb.Provenance{Identity: key, Etag: outboxProtoText(value.ETag), Absent: value.Absent, Unknown: value.Unknown}
			message.Provenance = append(message.Provenance, &values[index])
		}
	}
	var err error
	message.Entries, err = outboxProtoEntries(state.entries)
	if err != nil {
		return nil, err
	}
	message.Prepared, err = outboxProtoEntries(state.prepared)
	if err != nil {
		return nil, err
	}
	data, err := (proto.MarshalOptions{Deterministic: true}).MarshalAppend(make([]byte, outboxProtoPrefixSize), message)
	if err != nil {
		return nil, err
	}
	if uint64(len(data)-outboxProtoPrefixSize) > math.MaxUint32 {
		return nil, fmt.Errorf("outbox exceeds format size limit")
	}
	copy(data, outboxProtoMagic)
	binary.LittleEndian.PutUint32(data[len(outboxProtoMagic):], uint32(len(data)-outboxProtoPrefixSize))
	return data, nil
}

func outboxProtoText(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return string([]rune(value))
}

func outboxProtoEntries(entries map[string]OutboxEntry) ([]*outboxpb.Entry, error) {
	result := make([]*outboxpb.Entry, 0, len(entries))
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		entry, err := outboxProtoEntry(key, entries[key])
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, nil
}

func outboxProtoEntry(key string, e OutboxEntry) (*outboxpb.Entry, error) {
	times := [4]string{}
	for i, value := range [4]time.Time{e.CreatedAt, e.NextAttempt, e.ClaimUntil, e.PreparedUntil} {
		if value == (time.Time{}) {
			continue
		}
		var buffer [64]byte
		data, err := value.AppendText(buffer[:0])
		if err != nil {
			return nil, err
		}
		times[i] = string(data)
	}
	return &outboxpb.Entry{
		Identity: key, Id: outboxProtoText(e.ID), Operation: outboxProtoText(string(e.Operation)), Bucket: outboxProtoText(e.Bucket), Key: outboxProtoText(e.Key),
		SourceBucket: outboxProtoText(e.SourceBucket), SourceKey: outboxProtoText(e.SourceKey), Version: outboxProtoText(e.Version), PreviousVersion: outboxProtoText(e.PreviousVersion),
		UpstreamVersion: outboxProtoText(e.UpstreamVersion), UpstreamAbsent: e.UpstreamAbsent, Prepared: e.Prepared,
		CreatedAt: times[0], Attempts: int64(e.Attempts), NextAttempt: times[1], LastError: outboxProtoText(e.LastError), Terminal: e.Terminal,
		ClaimOwner: outboxProtoText(e.ClaimOwner), ClaimUntil: times[2], ClaimToken: e.ClaimToken, Attempted: e.Attempted,
		PreparedOwner: outboxProtoText(e.PreparedOwner), PreparedUntil: times[3], PreparedToken: e.PreparedToken,
	}, nil
}

func decodeOutboxProto(data []byte) (outboxState, error) {
	if len(data) < outboxProtoPrefixSize || string(data[:len(outboxProtoMagic)]) != outboxProtoMagic {
		return outboxState{}, fmt.Errorf("invalid protobuf outbox prefix")
	}
	size := binary.LittleEndian.Uint32(data[len(outboxProtoMagic):outboxProtoPrefixSize])
	if uint64(size) != uint64(len(data)-outboxProtoPrefixSize) {
		return outboxState{}, fmt.Errorf("invalid protobuf outbox length")
	}
	return decodeOutboxProtoRecords(data[outboxProtoPrefixSize:])
}

func decodeOutboxProtoEntry(e *outboxpb.Entry) (OutboxEntry, error) {
	times := [4]time.Time{}
	for i, value := range [4]string{e.CreatedAt, e.NextAttempt, e.ClaimUntil, e.PreparedUntil} {
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return OutboxEntry{}, fmt.Errorf("decode outbox timestamp: %w", err)
		}
		times[i] = parsed
	}
	if int64(int(e.Attempts)) != e.Attempts {
		return OutboxEntry{}, fmt.Errorf("outbox attempt count exceeds integer range")
	}
	return OutboxEntry{
		ID: e.Id, Operation: OutboxOperation(e.Operation), Bucket: e.Bucket, Key: e.Key, SourceBucket: e.SourceBucket, SourceKey: e.SourceKey,
		Version: e.Version, PreviousVersion: e.PreviousVersion, UpstreamVersion: e.UpstreamVersion, UpstreamAbsent: e.UpstreamAbsent, Prepared: e.Prepared,
		CreatedAt: times[0], Attempts: int(e.Attempts), NextAttempt: times[1], LastError: e.LastError, Terminal: e.Terminal,
		ClaimOwner: e.ClaimOwner, ClaimUntil: times[2], ClaimToken: e.ClaimToken, Attempted: e.Attempted,
		PreparedOwner: e.PreparedOwner, PreparedUntil: times[3], PreparedToken: e.PreparedToken,
	}, nil
}

func normalizeDecodedOutboxState(state outboxState) (outboxState, error) {
	if state.entries == nil {
		state.entries = make(map[string]OutboxEntry)
	}
	if state.prepared == nil {
		state.prepared = make(map[string]OutboxEntry)
	}
	for id := range state.prepared {
		if _, exists := state.entries[id]; exists {
			return outboxState{}, fmt.Errorf("outbox entry %q is both prepared and active", id)
		}
	}
	for id, entry := range state.entries {
		if !entry.Prepared {
			continue
		}
		delete(state.entries, id)
		state.prepared[id] = entry
	}
	if state.provenance == nil {
		state.provenance = make(map[string]UpstreamState)
	}
	if err := normalizeOutboxEntries(state.entries); err != nil {
		return outboxState{}, err
	}
	if err := normalizeOutboxEntries(state.prepared); err != nil {
		return outboxState{}, err
	}
	return state, nil
}
