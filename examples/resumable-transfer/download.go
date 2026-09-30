package resumetransfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type Downloader struct {
	Client          *s3.Client
	StatePath, Path string
	MaxBytes        int64
}
type rangeResponse struct {
	body               io.ReadCloser
	status             int
	etag, contentRange string
	length             int64
}

func (d Downloader) Start(ctx context.Context, target Target, expectedSHA string) (Progress, error) {
	p := Progress{Version: 1, Kind: "download", Target: target, SHA256: expectedSHA, Phase: "downloading"}
	if !validDigest(expectedSHA) {
		return p, ErrProgress
	}
	if err := requireNewState(d.StatePath); err != nil {
		return p, err
	}
	head, err := d.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(target.Bucket), Key: aws.String(target.Key)})
	if err != nil {
		return p, err
	}
	p.Size = aws.ToInt64(head.ContentLength)
	p.ETag = aws.ToString(head.ETag)
	if p.Size <= 0 || p.Size > transferLimit(d.MaxBytes) || p.ETag == "" {
		return p, ErrProgress
	}
	file, err := os.OpenFile(d.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return p, err
	}
	err = errors.Join(file.Sync(), file.Close())
	if err != nil {
		return p, err
	}
	if err = syncDirectory(filepath.Dir(d.Path)); err != nil {
		return p, err
	}
	empty := sha256.Sum256(nil)
	p.PrefixSHA256 = hex.EncodeToString(empty[:])
	return p, saveState(d.StatePath, p)
}
func (d Downloader) Resume(ctx context.Context, maxBytes int64) (Progress, error) {
	p, err := d.downloadState()
	if err != nil {
		return p, err
	}
	if p.Offset == p.Size {
		return p, nil
	}
	last := rangeLast(p, maxBytes)
	out, err := d.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(p.Target.Bucket), Key: aws.String(p.Target.Key), IfMatch: aws.String(p.ETag), Range: aws.String(fmt.Sprintf("bytes=%d-%d", p.Offset, last))})
	if err != nil {
		return p, err
	}
	status := 0
	if raw, ok := middleware.GetRawResponse(out.ResultMetadata).(*smithyhttp.Response); ok {
		status = raw.StatusCode
	}
	return d.acceptRange(p, last, rangeResponse{out.Body, status, aws.ToString(out.ETag), aws.ToString(out.ContentRange), aws.ToInt64(out.ContentLength)})
}
func (d Downloader) ResumeURL(ctx context.Context, url string, maxBytes int64) (Progress, error) {
	p, err := d.downloadState()
	if err != nil {
		return p, err
	}
	if p.Offset == p.Size {
		return p, nil
	}
	last := rangeLast(p, maxBytes)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return p, ErrProgress
	}
	req.Header.Set("If-Match", p.ETag)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", p.Offset, last))
	out, err := http.DefaultClient.Do(req)
	if err != nil {
		return p, errors.New("range transport unavailable")
	}
	length, _ := strconv.ParseInt(out.Header.Get("Content-Length"), 10, 64)
	return d.acceptRange(p, last, rangeResponse{out.Body, out.StatusCode, out.Header.Get("ETag"), out.Header.Get("Content-Range"), length})
}
func (d Downloader) downloadState() (Progress, error) {
	p, err := loadState(d.StatePath, "download")
	if err != nil {
		return p, err
	}
	if p.Size > transferLimit(d.MaxBytes) || p.ETag == "" || !validDigest(p.PrefixSHA256) || (p.Phase != "downloading" && p.Phase != "complete") {
		return p, ErrProgress
	}
	sha, size, err := sourceIdentity(d.Path, transferLimit(d.MaxBytes))
	if err != nil {
		return p, err
	}
	if sha != p.PrefixSHA256 || size != p.Offset {
		return p, ErrChanged
	}
	if p.Offset == p.Size && sha != p.SHA256 {
		return p, ErrChanged
	}
	return p, nil
}
func rangeLast(p Progress, maxBytes int64) int64 {
	if maxBytes <= 0 || maxBytes > 8<<20 {
		maxBytes = 8 << 20
	}
	return min(p.Size-1, p.Offset+maxBytes-1)
}
func (d Downloader) acceptRange(p Progress, last int64, response rangeResponse) (Progress, error) {
	defer response.body.Close()
	length := last - p.Offset + 1
	want := fmt.Sprintf("bytes %d-%d/%d", p.Offset, last, p.Size)
	if response.status != http.StatusPartialContent || response.etag != p.ETag || response.contentRange != want || response.length != length {
		return p, ErrChanged
	}
	data, err := io.ReadAll(io.LimitReader(response.body, length+1))
	if err != nil {
		return p, err
	}
	if int64(len(data)) != length {
		return p, ErrChanged
	}
	if last == p.Size-1 {
		if err = verifyFinalSuffix(d.Path, data, p.SHA256); err != nil {
			return p, err
		}
	}
	file, err := os.OpenFile(d.Path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return p, err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return p, err
	}
	sha, size, err := sourceIdentity(d.Path, transferLimit(d.MaxBytes))
	if err != nil {
		return p, err
	}
	p.Offset, p.PrefixSHA256 = size, sha
	if p.Offset == p.Size {
		if sha != p.SHA256 {
			return p, ErrChanged
		}
		p.Phase = "complete"
	}
	return p, saveState(d.StatePath, p)
}
func verifyFinalSuffix(path string, data []byte, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	sha, _, err := hashReader(io.MultiReader(file, bytes.NewReader(data)))
	if err != nil {
		return err
	}
	if sha != want {
		return ErrChanged
	}
	return nil
}
