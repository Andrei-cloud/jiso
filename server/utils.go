package main

import (
	"fmt"
	"io"
	"os"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/specs"
)

func CreateSpecFromFile(path string) (*iso8583.MessageSpec, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}
	defer fd.Close()

	raw, err := io.ReadAll(fd)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	return specs.Builder.ImportJSON(raw)
}
