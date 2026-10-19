package stow

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Reserve the selected immutable tree before fetching its payload. Fetched
// metadata is admitted against the same projection before checkout.
func admitGitTree(source, commit string, totals *seedContext) error {
	command := seedGitCommand(source, "ls-tree", "-rlz", "--full-tree", commit)
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	projected := *totals
	scanner := bufio.NewScanner(output)
	scanner.Split(splitGitTreeEntry)
	for scanner.Scan() {
		if err := admitGitTreeEntry(scanner.Text(), &projected); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("stow: admit Git seed: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	if err := command.Wait(); err != nil {
		return err
	}
	*totals = projected
	return nil
}

func admitGitTreeEntry(entry string, projected *seedContext) error {
	header, path, ok := strings.Cut(entry, "\t")
	fields := strings.Fields(header)
	if !ok || len(fields) != 4 {
		return fmt.Errorf("invalid Git tree entry")
	}
	// Non-recursive checkout leaves submodule directories empty.
	if fields[1] == "commit" {
		return nil
	}
	if fields[1] != "blob" || fields[0] != "100644" && fields[0] != "100755" {
		return fmt.Errorf("Git input supports regular files only: %s", path)
	}
	if !validPortableRelativePath(path) {
		return fmt.Errorf("Git input path is not portable: %s", path)
	}
	if !projected.includeSensitive && sensitiveSeedPath(path) {
		return fmt.Errorf("sensitive-looking Git input %s is excluded by default (set include_sensitive_inputs explicitly to include it)", path)
	}
	size, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return err
	}
	return projected.add(size, 1)
}

func splitGitTreeEntry(data []byte, atEOF bool) (int, []byte, error) {
	if index := bytes.IndexByte(data, 0); index >= 0 {
		return index + 1, data[:index], nil
	}
	if atEOF && len(data) != 0 {
		return 0, nil, fmt.Errorf("unterminated Git tree entry")
	}
	return 0, nil, nil
}
