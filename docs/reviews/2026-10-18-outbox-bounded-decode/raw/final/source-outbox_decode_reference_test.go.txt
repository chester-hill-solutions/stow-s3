package runthrough

import (
	"encoding/binary"
	"fmt"
	"github.com/chester-hill-solutions/stow-s3/internal/runthrough/outboxpb"
	"google.golang.org/protobuf/proto"
)

func referenceDecodeOutboxProto(data []byte) (outboxState, error) {
	if len(data) < outboxProtoPrefixSize || string(data[:len(outboxProtoMagic)]) != outboxProtoMagic {
		return outboxState{}, fmt.Errorf("invalid protobuf outbox prefix")
	}
	size := binary.LittleEndian.Uint32(data[len(outboxProtoMagic):outboxProtoPrefixSize])
	if uint64(size) != uint64(len(data)-outboxProtoPrefixSize) {
		return outboxState{}, fmt.Errorf("invalid protobuf outbox length")
	}
	message := &outboxpb.Outbox{}
	if err := proto.Unmarshal(data[outboxProtoPrefixSize:], message); err != nil {
		return outboxState{}, fmt.Errorf("decode protobuf outbox: %w", err)
	}
	if message.Version != outboxProtoVersion || len(message.ProtoReflect().GetUnknown()) != 0 {
		return outboxState{}, fmt.Errorf("%w: protobuf version %d or unknown fields", ErrOutboxFormatVersion, message.Version)
	}
	state := outboxState{provenance: make(map[string]UpstreamState, len(message.Provenance)), seq: message.Seq, nextToken: message.NextToken}
	for index, value := range message.Provenance {
		if len(value.ProtoReflect().GetUnknown()) != 0 {
			return outboxState{}, fmt.Errorf("%w: unknown provenance fields", ErrOutboxFormatVersion)
		}
		state.provenance[value.Identity] = UpstreamState{ETag: value.Etag, Absent: value.Absent, Unknown: value.Unknown}
		if len(state.provenance) != index+1 {
			return outboxState{}, fmt.Errorf("duplicate outbox provenance identity %q", value.Identity)
		}
	}
	var err error
	state.entries, err = referenceDecodeOutboxProtoEntries(message.Entries)
	if err != nil {
		return outboxState{}, err
	}
	state.prepared, err = referenceDecodeOutboxProtoEntries(message.Prepared)
	if err != nil {
		return outboxState{}, err
	}
	return normalizeDecodedOutboxState(state)
}

func referenceDecodeOutboxProtoEntries(entries []*outboxpb.Entry) (map[string]OutboxEntry, error) {
	result := make(map[string]OutboxEntry, len(entries))
	for index, entry := range entries {
		if len(entry.ProtoReflect().GetUnknown()) != 0 {
			return nil, fmt.Errorf("%w: unknown entry fields", ErrOutboxFormatVersion)
		}
		decoded, err := decodeOutboxProtoEntry(entry)
		if err != nil {
			return nil, err
		}
		result[entry.Identity] = decoded
		if len(result) != index+1 {
			return nil, fmt.Errorf("duplicate outbox entry identity %q", entry.Identity)
		}
	}
	return result, nil
}
