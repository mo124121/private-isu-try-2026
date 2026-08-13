package main

import (
	"io"
	"net/http"
	"strings"
)

type multipartPost struct {
	csrf     string
	body     string
	mime     string
	fileData []byte
}

type multipartPostError string

func (e multipartPostError) Error() string { return string(e) }

const (
	errMultipartMissingFile multipartPostError = "missing file"
	errMultipartUnsupported multipartPostError = "unsupported image type"
	errMultipartTooLarge    multipartPostError = "image too large"
)

// parsePostMultipart reads each multipart part once without ParseMultipartForm.
// Image bytes stay in memory until the single final write to the published
// image file, avoiding the extra temporary-file round trip for small uploads.
func parsePostMultipart(r *http.Request) (multipartPost, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return multipartPost{}, err
	}
	var result multipartPost
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return multipartPost{}, err
		}

		name := part.FormName()
		if part.FileName() == "" {
			value, readErr := io.ReadAll(io.LimitReader(part, 1<<20))
			part.Close()
			if readErr != nil {
				return multipartPost{}, readErr
			}
			switch name {
			case "csrf_token":
				result.csrf = string(value)
			case "body":
				result.body = string(value)
			}
			continue
		}

		if name != "file" || result.fileData != nil {
			_, _ = io.Copy(io.Discard, part)
			part.Close()
			continue
		}

		contentType := part.Header.Get("Content-Type")
		result.mime = imageMime(contentType)
		if result.mime == "" {
			part.Close()
			return multipartPost{}, errMultipartUnsupported
		}
		fileData, readErr := io.ReadAll(io.LimitReader(part, UploadLimit+1))
		part.Close()
		if readErr != nil {
			return multipartPost{}, readErr
		}
		if len(fileData) > UploadLimit {
			return multipartPost{}, errMultipartTooLarge
		}
		result.fileData = fileData
	}

	if result.fileData == nil {
		return multipartPost{}, errMultipartMissingFile
	}
	return result, nil
}

func imageMime(contentType string) string {
	contentType = strings.ToLower(contentType)
	switch {
	case strings.Contains(contentType, "jpeg"):
		return "image/jpeg"
	case strings.Contains(contentType, "png"):
		return "image/png"
	case strings.Contains(contentType, "gif"):
		return "image/gif"
	default:
		return ""
	}
}

func multipartErrorMessage(err error) string {
	switch err {
	case errMultipartMissingFile:
		return "画像が必須です"
	case errMultipartUnsupported:
		return "投稿できる画像形式はjpgとpngとgifだけです"
	case errMultipartTooLarge:
		return "ファイルサイズが大きすぎます"
	default:
		return "投稿データを読み込めませんでした"
	}
}
