package runthrough

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"testing"
)

func TestUpstreamChecksumsPreserveFullObjectOnly(t *testing.T) {
	full, err := headOutputToMeta("bucket", "key", &s3.HeadObjectOutput{ChecksumCRC64NVME: aws.String("jSnVw/bqjr4="), ChecksumType: types.ChecksumTypeFullObject})
	if err != nil || full.ChecksumAlgorithm != "CRC64NVME" || full.ChecksumValue != "jSnVw/bqjr4=" {
		t.Fatalf("full = %v, %v", full, err)
	}
	composite, err := getOutputToMeta("bucket", "key", &s3.GetObjectOutput{ChecksumCRC32: aws.String("AAAAAA==-2"), ChecksumType: types.ChecksumTypeComposite})
	if err != nil || composite.ChecksumAlgorithm != "" || composite.ChecksumValue != "" {
		t.Fatalf("composite = %v, %v", composite, err)
	}
}
