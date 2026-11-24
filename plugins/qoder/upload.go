package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxUploadChunk = 96 << 10
	maxUploads     = 4
	uploadPrefix   = "handoff-upload:"
)

type fileUpload struct {
	path    string
	size    int64
	written int64
	touched time.Time
}

func (s *server) upload(input map[string]string) (map[string]any, error) {
	now := time.Now()
	s.expireUploads(now)
	id := input["upload_id"]
	if id == "" {
		size, err := strconv.ParseInt(input["size"], 10, 64)
		if err != nil || size <= 0 || size > maxZIPBytes {
			return nil, fmt.Errorf("choose a ZIP no larger than %d bytes", maxZIPBytes)
		}
		if len(s.uploads) >= maxUploads {
			return nil, fmt.Errorf("too many ZIP files are open; reopen the panel or wait for a previous file to expire")
		}
		id = uuid.NewString()
		path := filepath.Join(s.temp, id+".upload.zip")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			return nil, err
		}
		s.uploads[id] = &fileUpload{path: path, size: size, touched: now}
		return map[string]any{"upload_id": id}, nil
	}

	current := s.uploads[id]
	if current == nil || current.written == current.size {
		return nil, fmt.Errorf("ZIP upload is unavailable; choose the file again")
	}
	value := input["data"]
	if len(value) > base64.StdEncoding.EncodedLen(maxUploadChunk) {
		return nil, fmt.Errorf("ZIP chunk exceeds the transport limit")
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(data) == 0 || current.written+int64(len(data)) > current.size {
		return nil, fmt.Errorf("invalid ZIP chunk")
	}
	file, err := os.OpenFile(current.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(current.path)
		delete(s.uploads, id)
		return nil, fmt.Errorf("could not receive ZIP; choose the file again")
	}
	current.written += int64(len(data))
	current.touched = now
	result := map[string]any{"upload_id": id}
	if current.written == current.size {
		result["source"] = uploadPrefix + id
	}
	return result, nil
}

func (s *server) uploadedZIP(source string, now time.Time) (string, error) {
	s.expireUploads(now)
	file := s.uploads[strings.TrimPrefix(source, uploadPrefix)]
	if file == nil || file.written != file.size {
		return "", fmt.Errorf("choose the ZIP file again")
	}
	file.touched = now
	return file.path, nil
}

func (s *server) expireUploads(now time.Time) {
	for id, file := range s.uploads {
		if now.Sub(file.touched) >= ticketLifetime {
			_ = os.Remove(file.path)
			delete(s.uploads, id)
		}
	}
}
