//go:build unix || windows

package ready_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/ready"
)

func TestReadinessPreservesBorrowedDescriptorOwnership(t *testing.T) {
	kinds := []string{"file", "pipe", "write-error"}
	if runtime.GOOS != "windows" {
		kinds = append(kinds, "reuse")
	}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReadinessDescriptorProbe$")
			command.Env = append(os.Environ(), "STOW_TEST_READY_FD_PROBE="+kind)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("descriptor probe exceeded its deadline")
			}
			if err != nil || !strings.Contains(string(output), "descriptor probe passed") {
				t.Fatalf("isolated %s probe: %v\n%s", kind, err, output)
			}
		})
	}
}

func TestReadinessDescriptorProbe(t *testing.T) {
	kind := os.Getenv("STOW_TEST_READY_FD_PROBE")
	if kind == "" {
		return
	}
	if err := probeReadinessDescriptor(kind); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("descriptor probe passed")
	os.Exit(0)
}

func probeReadinessDescriptor(kind string) error {
	owner, err := os.CreateTemp("", "stow-ready-owner-*")
	if err != nil {
		return err
	}
	defer os.Remove(owner.Name())
	defer owner.Close()
	if kind == "pipe" {
		reader, writer, err := os.Pipe()
		if err != nil {
			return err
		}
		defer reader.Close()
		defer writer.Close()
		return retainReadyDescriptor(writer, false)
	}
	if kind == "write-error" {
		reader, err := os.Open(owner.Name())
		if err != nil {
			return err
		}
		defer reader.Close()
		return retainReadyDescriptor(reader, true)
	}
	if kind != "reuse" {
		return retainReadyDescriptor(owner, false)
	}
	descriptor := owner.Fd()
	if err := ready.WriteToFd(int(descriptor), sampleMessage()); err != nil {
		return err
	}
	if err := owner.Close(); err != nil {
		return err
	}
	replacement, err := os.CreateTemp("", "stow-ready-replacement-*")
	if err != nil {
		return err
	}
	defer os.Remove(replacement.Name())
	defer replacement.Close()
	if replacement.Fd() != descriptor {
		return fmt.Errorf("reuse fixture did not regain descriptor %d", descriptor)
	}
	return assertReadyOwnerSurvivesGC(replacement, false)
}

func retainReadyDescriptor(owner *os.File, readOnly bool) error {
	err := ready.WriteToFd(int(owner.Fd()), sampleMessage())
	if (err != nil) != readOnly {
		return fmt.Errorf("readOnly=%t readiness write error: %v", readOnly, err)
	}
	return assertReadyOwnerSurvivesGC(owner, readOnly)
}

func assertReadyOwnerSurvivesGC(owner *os.File, readOnly bool) error {
	for range 5 {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		if _, err := owner.Stat(); err != nil {
			return fmt.Errorf("GC closed caller descriptor: %w", err)
		}
		if !readOnly {
			if _, err := owner.WriteString("caller-owned bytes\n"); err != nil {
				return fmt.Errorf("caller cannot write after GC: %w", err)
			}
		}
	}
	runtime.KeepAlive(owner)
	return nil
}
