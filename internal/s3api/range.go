package s3api

import (
	"strconv"
	"strings"
)

func parseRange(hdr string, size int64) (start, end int64, err error) {
	if size <= 0 || !strings.HasPrefix(hdr, "bytes=") {
		return 0, 0, errInvalidRange
	}
	spec := strings.TrimPrefix(hdr, "bytes=")
	if !validByteRange(spec) {
		return 0, 0, errInvalidRange
	}
	if strings.HasPrefix(spec, "-") {
		return parseSuffixRange(spec[1:], size)
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, errInvalidRange
	}
	s, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, errInvalidRange
	}
	var e int64
	if parts[1] == "" {
		e = size - 1
	} else {
		e, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, errInvalidRange
		}
	}
	if s < 0 || s >= size || e < s {
		return 0, 0, errInvalidRange
	}
	// An end past the last byte is clamped, not refused. A recipient must treat an
	// unsatisfiable end as the last byte, and clients that ask for "the rest of
	// this" routinely name an offset they inferred rather than measured. Only a
	// range that *begins* past the end is unsatisfiable, which the check above
	// rejects.
	if e >= size {
		e = size - 1
	}
	return s, e, nil
}

var errInvalidRange = &rangeError{}

type rangeError struct{}

func (e *rangeError) Error() string { return "invalid range" }

func validByteRange(spec string) bool {
	for _, ch := range spec {
		if (ch < '0' || ch > '9') && ch != '-' {
			return false
		}
	}
	return true
}

func parseSuffixRange(spec string, size int64) (int64, int64, error) {
	n, err := strconv.ParseInt(spec, 10, 64)
	if err != nil || n <= 0 {
		return 0, 0, errInvalidRange
	}
	if n > size {
		n = size
	}
	return size - n, size - 1, nil
}
