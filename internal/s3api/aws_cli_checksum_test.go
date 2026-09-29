package s3api_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAWSCLIDefaultCRC64NVME(t *testing.T) {
	if os.Getenv("STOW_TEST_AWS_CLI") != "1" {
		t.Skip("set STOW_TEST_AWS_CLI=1 to run the installed AWS CLI compatibility profile")
	}
	binary, err := exec.LookPath("aws")
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t)
	defer server.Close()
	body := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(body, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	runAWSCLI(t, binary, server.URL, []string{"create-bucket", "--bucket", "checksum-bucket"})
	output := runAWSCLI(t, binary, server.URL, []string{"put-object", "--bucket", "checksum-bucket", "--key", "object", "--body", body, "--metadata", "project=stow"})
	var result struct{ ChecksumCRC64NVME string }
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result.ChecksumCRC64NVME != "jSnVw/bqjr4=" {
		t.Fatalf("default checksum = %s", output)
	}
	output = runAWSCLI(t, binary, server.URL, []string{"head-object", "--bucket", "checksum-bucket", "--key", "object"})
	var head struct {
		Metadata          map[string]string
		ChecksumCRC64NVME string
	}
	if err := json.Unmarshal(output, &head); err != nil {
		t.Fatal(err)
	}
	if head.Metadata["project"] != "stow" || head.ChecksumCRC64NVME != "jSnVw/bqjr4=" {
		t.Fatalf("HEAD = %s", output)
	}
}

func runAWSCLI(t *testing.T, binary, endpoint string, args []string) []byte {
	t.Helper()
	flags := []string{"s3api", "--endpoint-url", endpoint, "--no-sign-request", "--region", "us-east-1", "--no-cli-pager"}
	command := exec.Command(binary, append(flags, args...)...)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "AWS_") {
			command.Env = append(command.Env, item)
		}
	}
	command.Env = append(command.Env, "AWS_CONFIG_FILE=/dev/null", "AWS_SHARED_CREDENTIALS_FILE=/dev/null", "AWS_EC2_METADATA_DISABLED=true")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("AWS CLI %s: %v: %s", args[0], err, output)
	}
	return output
}
