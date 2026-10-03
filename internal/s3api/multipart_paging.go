package s3api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func listPartsOptions(q url.Values) (storage.ListPartsOptions, error) {
	opts := storage.ListPartsOptions{MaxParts: 1000}
	for _, field := range []struct {
		name   string
		target *int
		max    int
	}{{"max-parts", &opts.MaxParts, 1000}, {"part-number-marker", &opts.PartNumberMarker, 10000}} {
		if raw, ok := q[field.name]; ok {
			if len(raw) != 1 {
				return opts, fmt.Errorf("Invalid %s", field.name)
			}
			n, err := strconv.Atoi(raw[0])
			if err != nil || n < 0 || (field.name == "max-parts" && n == 0) || n > field.max {
				return opts, fmt.Errorf("Invalid %s", field.name)
			}
			*field.target = n
		}
	}
	return opts, nil
}

type multipartPartsPager interface {
	ListPartsPage(context.Context, string, storage.ListPartsOptions) (*storage.ListPartsResult, error)
}

func (s *Server) listPartsPage(ctx context.Context, uploadID string, opts storage.ListPartsOptions) (*storage.ListPartsResult, error) {
	if pager, ok := s.multipart.(multipartPartsPager); ok {
		return pager.ListPartsPage(ctx, uploadID, opts)
	}
	parts, err := s.multipart.ListParts(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	return storage.PaginateParts(parts, opts), nil
}
