package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

const imageExportBatchSize = 100

type imageRow struct {
	ID      int64  `db:"id"`
	Imgdata []byte `db:"imgdata"`
	Mime    string `db:"mime"`
}

// exportImages materializes all DB images for nginx static delivery.
func exportImages(ctx context.Context, conn *sqlx.DB, dirName string) error {
	if err := os.RemoveAll(dirName); err != nil {
		return fmt.Errorf("reset image directory: %w", err)
	}
	if err := os.MkdirAll(dirName, 0755); err != nil {
		return fmt.Errorf("create image directory: %w", err)
	}

	var rows []imageRow
	var seekID int64
	for {
		rows = rows[:0]
		if err := conn.SelectContext(ctx, &rows,
			"SELECT id, imgdata, mime FROM posts WHERE id > ? ORDER BY id LIMIT ?",
			seekID, imageExportBatchSize,
		); err != nil {
			return fmt.Errorf("load images for export: %w", err)
		}

		for _, row := range rows {
			if err := saveImageFile(dirName, row.ID, row.Mime, row.Imgdata); err != nil {
				return fmt.Errorf("save image %d: %w", row.ID, err)
			}
		}
		if len(rows) < imageExportBatchSize {
			return nil
		}
		seekID = rows[len(rows)-1].ID
	}
}

func cleanupGeneratedImages(dirName string, maxID int64) error {
	entries, err := os.ReadDir(dirName)
	if os.IsNotExist(err) {
		return os.MkdirAll(dirName, 0755)
	}
	if err != nil {
		return fmt.Errorf("read image directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		idText := name
		if dot := strings.IndexByte(name, '.'); dot > 0 {
			idText = name[:dot]
		}
		id, err := strconv.ParseInt(idText, 10, 64)
		if err != nil || id <= maxID {
			continue
		}
		if err := os.Remove(filepath.Join(dirName, name)); err != nil {
			return fmt.Errorf("remove generated image %q: %w", name, err)
		}
	}
	return nil
}

// saveImageFile writes an image atomically so nginx never observes a partial file.
func saveImageFile(dirName string, id int64, mime string, data []byte) error {
	ext, ok := imageExtension(mime)
	if !ok {
		return fmt.Errorf("unsupported image mime type %q", mime)
	}
	if err := os.MkdirAll(dirName, 0755); err != nil {
		return fmt.Errorf("create image directory: %w", err)
	}

	tmp, err := os.CreateTemp(dirName, fmt.Sprintf(".%d-*", id))
	if err != nil {
		return fmt.Errorf("create temporary image: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary image: %w", err)
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write image: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary image: %w", err)
	}

	if err := os.Rename(tmpName, filepath.Join(dirName, fmt.Sprintf("%d.%s", id, ext))); err != nil {
		return fmt.Errorf("publish image: %w", err)
	}
	return nil
}

func imageExtension(mime string) (string, bool) {
	switch mime {
	case "image/jpeg":
		return "jpg", true
	case "image/png":
		return "png", true
	case "image/gif":
		return "gif", true
	default:
		return "", false
	}
}
