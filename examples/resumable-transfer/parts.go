package resumetransfer

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (u Uploader) remoteParts(ctx context.Context, p Progress) (map[int32]types.Part, error) {
	parts := make(map[int32]types.Part)
	input := &s3.ListPartsInput{Bucket: aws.String(p.Target.Bucket), Key: aws.String(p.Target.Key), UploadId: aws.String(p.UploadID), MaxParts: aws.Int32(1)}
	for pages := 0; pages < 10000; pages++ {
		out, err := u.Client.ListParts(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, part := range out.Parts {
			number := aws.ToInt32(part.PartNumber)
			if number < 1 || number > 10000 {
				return nil, ErrProgress
			}
			if _, found := parts[number]; found {
				return nil, ErrProgress
			}
			parts[number] = part
		}
		if !aws.ToBool(out.IsTruncated) {
			return parts, nil
		}
		next := aws.ToString(out.NextPartNumberMarker)
		if next == "" || next == aws.ToString(input.PartNumberMarker) {
			return nil, ErrProgress
		}
		input.PartNumberMarker = aws.String(next)
	}
	return nil, ErrProgress
}

// sendMissing uploads the parts the remote is missing, up to maxNew of them,
// and returns the parts the completion would name. It reports how many parts it
// actually sent so the caller can treat sending and completing as separate
// units of work: a call that sent a part is not the call that finishes the
// upload, which keeps the completion a single retryable step against parts
// that are already durable.
func (u Uploader) sendMissing(ctx context.Context, file *os.File, p Progress, remote map[int32]types.Part, maxNew int) ([]types.CompletedPart, int, error) {
	count := partCount(p)
	if int64(len(remote)) > count {
		return nil, 0, ErrChanged
	}
	completed := make([]types.CompletedPart, 0, count)
	sent := 0
	for number := int32(1); int64(number) <= count; number++ {
		data, err := sourcePart(file, p, number)
		if err != nil {
			return nil, sent, err
		}
		tag := fmt.Sprintf("%x", md5.Sum(data))
		part, found := remote[number]
		if found {
			if aws.ToInt64(part.Size) != int64(len(data)) || strings.Trim(aws.ToString(part.ETag), "\"") != tag {
				return nil, sent, ErrChanged
			}
		}
		if !found {
			if maxNew > 0 && sent >= maxNew {
				return completed, sent, nil
			}
			out, err := u.Client.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(p.Target.Bucket), Key: aws.String(p.Target.Key), UploadId: aws.String(p.UploadID), PartNumber: aws.Int32(number), Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data)))})
			if err != nil {
				return nil, sent, err
			}
			if strings.Trim(aws.ToString(out.ETag), "\"") != tag {
				return nil, sent, ErrChanged
			}
			part.ETag = out.ETag
			sent++
		}
		completed = append(completed, types.CompletedPart{PartNumber: aws.Int32(number), ETag: part.ETag})
	}
	return completed, sent, nil
}

func partCount(p Progress) int64 { return (p.Size + p.PartSize - 1) / p.PartSize }

// sourcePart reads one part of the source. A part number outside the transfer is
// refused rather than read: a caller that disagrees with partCount about how
// many parts there are gets an error, not a negative-length read.
func sourcePart(file *os.File, p Progress, number int32) ([]byte, error) {
	offset := (int64(number) - 1) * p.PartSize
	size := min(p.PartSize, p.Size-offset)
	if number < 1 || size <= 0 {
		return nil, ErrProgress
	}
	data := make([]byte, size)
	_, err := io.ReadFull(io.NewSectionReader(file, offset, size), data)
	return data, err
}
