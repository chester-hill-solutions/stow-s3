package resumetransfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type Uploader struct {
	Client             *s3.Client
	StatePath          string
	PartSize, MaxBytes int64
}

func (u Uploader) Start(ctx context.Context, source string, target Target) (Progress, error) {
	p := Progress{Version: 1, Kind: "upload", Target: target, Phase: "uploading", PartSize: u.PartSize}
	if p.PartSize == 0 {
		p.PartSize = 5 << 20
	}
	if p.PartSize < 5<<20 || p.PartSize > 64<<20 || target.Bucket == "" || target.Key == "" {
		return p, ErrProgress
	}
	if err := requireNewState(u.StatePath); err != nil {
		return p, err
	}
	sha, size, err := sourceIdentity(source, transferLimit(u.MaxBytes))
	if err != nil {
		return p, err
	}
	if size == 0 {
		return p, ErrProgress
	}
	p.SHA256, p.Size = sha, size
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return p, err
	}
	p.TransferID = hex.EncodeToString(id[:])
	created, err := u.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(target.Bucket), Key: aws.String(target.Key), ContentType: aws.String("application/octet-stream"), Metadata: uploadMetadata(p)})
	if err != nil {
		return p, err
	}
	p.UploadID = aws.ToString(created.UploadId)
	if p.UploadID == "" {
		return p, ErrProgress
	}
	return p, saveState(u.StatePath, p)
}
func uploadMetadata(p Progress) map[string]string {
	return map[string]string{"resume-sha256": p.SHA256, "resume-transfer-id": p.TransferID}
}

func (u Uploader) Resume(ctx context.Context, source string, maxNewParts int) (Progress, error) {
	p, err := loadState(u.StatePath, "upload")
	if err != nil {
		return p, err
	}
	if err = validateUpload(p); err != nil {
		return p, err
	}
	sha, size, err := sourceIdentity(source, transferLimit(u.MaxBytes))
	if err != nil {
		return p, err
	}
	if sha != p.SHA256 || size != p.Size {
		return p, ErrChanged
	}
	if p.Phase == "complete" {
		return p, u.verifyComplete(ctx, p)
	}
	parts, err := u.remoteParts(ctx, p)
	if isMissingUpload(err) {
		if p.Phase != "completing" {
			return p, ErrRemoteLost
		}
		return u.resolveComplete(ctx, p)
	}
	if err != nil {
		return p, err
	}
	f, err := os.Open(source)
	if err != nil {
		return p, err
	}
	defer f.Close()
	completed, sent, err := u.sendMissing(ctx, f, p, parts, maxNewParts)
	if err != nil {
		return p, err
	}
	// Sending and completing are separate calls. A call that sent a part, or
	// that ran out of its part budget, returns with the upload still open, so
	// the completion is never bundled with bytes that may still be in doubt.
	if sent > 0 || int64(len(completed)) < partCount(p) {
		return p, nil
	}
	sha, size, err = sourceIdentity(source, transferLimit(u.MaxBytes))
	if err != nil {
		return p, err
	}
	if sha != p.SHA256 || size != p.Size {
		return p, ErrChanged
	}
	p.Phase = "completing"
	if err = saveState(u.StatePath, p); err != nil {
		return p, err
	}
	_, err = u.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String(p.Target.Bucket), Key: aws.String(p.Target.Key), UploadId: aws.String(p.UploadID), MultipartUpload: &types.CompletedMultipartUpload{Parts: completed}})
	if err != nil {
		return p, err
	}
	return u.resolveComplete(ctx, p)
}
func validateUpload(p Progress) error {
	if p.UploadID == "" || len(p.UploadID) > 1024 || len(p.TransferID) != 32 || p.PartSize < 5<<20 || p.PartSize > 64<<20 {
		return ErrProgress
	}
	if p.Phase != "uploading" && p.Phase != "completing" && p.Phase != "complete" {
		return ErrProgress
	}
	return nil
}
func isMissingUpload(err error) bool {
	var api smithy.APIError
	return errors.As(err, &api) && api.ErrorCode() == "NoSuchUpload"
}
func (u Uploader) resolveComplete(ctx context.Context, p Progress) (Progress, error) {
	if err := u.verifyComplete(ctx, p); err != nil {
		return p, err
	}
	p.Phase = "complete"
	return p, saveState(u.StatePath, p)
}
func (u Uploader) verifyComplete(ctx context.Context, p Progress) error {
	head, err := u.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(p.Target.Bucket), Key: aws.String(p.Target.Key)})
	if err != nil {
		return err
	}
	if aws.ToInt64(head.ContentLength) != p.Size || aws.ToString(head.ContentType) != "application/octet-stream" || head.Metadata["resume-sha256"] != p.SHA256 || head.Metadata["resume-transfer-id"] != p.TransferID || aws.ToString(head.ETag) == "" {
		return ErrChanged
	}
	object, err := u.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(p.Target.Bucket), Key: aws.String(p.Target.Key), IfMatch: head.ETag})
	if err != nil {
		return err
	}
	defer object.Body.Close()
	sha, size, err := hashReader(io.LimitReader(object.Body, p.Size+1))
	if err != nil {
		return err
	}
	if sha != p.SHA256 || size != p.Size {
		return ErrChanged
	}
	return nil
}
