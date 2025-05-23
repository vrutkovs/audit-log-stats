package jsonl

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type Reader struct {
	r       io.Reader
	scanner *bufio.Scanner
}

const maxCapacity = 30 * 1024 * 1024 // 512KB, adjust as needed

func NewReader(r io.Reader) Reader {
	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)

	// Open with large buffer as some CSV lines can be very long
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	return Reader{
		r:       r,
		scanner: scanner,
	}
}

func (r Reader) Close() error {
	if c, ok := r.r.(io.ReadCloser); ok {
		return c.Close()
	}
	return fmt.Errorf("given reader is no ReadCloser")
}

func (r Reader) ReadSingleLine(output interface{}) error {
	ok := r.scanner.Scan()
	if !ok {
		return fmt.Errorf("could not read from scanner: %w", r.scanner.Err())
	}

	return json.Unmarshal(r.scanner.Bytes(), output)
}

func (r Reader) ReadLines(callback func(data []byte) error) error {
	for r.scanner.Scan() {
		err := callback(r.scanner.Bytes())
		if err != nil {
			return fmt.Errorf("error in callback: %w", err)
		}
	}
	return r.scanner.Err()
}
