package util

import (
	"fmt"
	"io"
	"os"
)

func ReadFile(path string) ([]byte, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error read file: %v", err)
	}
	return bytes, nil
}

func MustReadFile(path string) []byte {
	return MustAnyByteSlice(ReadFile(path))
}

func FileExists(file string) bool {
	_, err := os.Stat(file)
	if os.IsNotExist(err) {
		return false
	}
	Must(err)
	return true
}

func MustInitFile(file string) io.ReadCloser {
	if !FileExists(file) {
		f, _ := os.Create(file)
		Must(f.Close())
	}
	return MustNotNil[io.ReadCloser](os.Open(file))
}

func MustOpenFile(file string) io.WriteCloser {
	Must(MustInitFile(file))
	return MustNotNil[io.WriteCloser](os.OpenFile(file, os.O_WRONLY|os.O_TRUNC, 0666))
}
