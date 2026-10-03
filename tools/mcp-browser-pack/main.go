package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := pack(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func pack(input io.Reader, output io.Writer) error {
	const maximum = 4 << 20
	data, err := io.ReadAll(io.LimitReader(input, maximum+1))
	if err != nil {
		return err
	}
	if len(data) > maximum {
		return fmt.Errorf("browser HTML exceeds the build limit")
	}
	writer, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	return writer.Close()
}
