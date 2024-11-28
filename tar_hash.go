package acbrun

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

func GetTarSha256String(path string) (string, error) {
	r, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	uncompressedReader, err := gzip.NewReader(r)
	if err != nil {
		return "", err
	}
	defer uncompressedReader.Close()
	h := sha256.New()
	if _, err := io.Copy(h, uncompressedReader); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
