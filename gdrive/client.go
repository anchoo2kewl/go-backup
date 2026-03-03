package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
)

const (
	driveFilesURL  = "https://www.googleapis.com/drive/v3/files"
	driveUploadURL = "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart"
)

// driveFile is the partial Drive API file metadata.
type driveFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Parents  []string `json:"parents,omitempty"`
	WebViewLink string `json:"webViewLink,omitempty"`
	Size     string `json:"size,omitempty"`
}

// uploadFile uploads data from r (with known size) to Google Drive.
// Returns the Drive file ID and web view link.
func uploadFile(ctx context.Context, client *http.Client, name, parentID string, r io.Reader, size int64) (string, string, error) {
	// Build the multipart body: JSON metadata part + binary data part.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Part 1: metadata
	mh := make(textproto.MIMEHeader)
	mh.Set("Content-Type", "application/json; charset=UTF-8")
	metaPart, err := mw.CreatePart(mh)
	if err != nil {
		return "", "", fmt.Errorf("gdrive: failed to create metadata part: %w", err)
	}
	meta := driveFile{Name: name, MimeType: "application/gzip"}
	if parentID != "" {
		meta.Parents = []string{parentID}
	}
	if err := json.NewEncoder(metaPart).Encode(meta); err != nil {
		return "", "", fmt.Errorf("gdrive: failed to encode metadata: %w", err)
	}

	// Part 2: binary data
	dh := make(textproto.MIMEHeader)
	dh.Set("Content-Type", "application/gzip")
	dataPart, err := mw.CreatePart(dh)
	if err != nil {
		return "", "", fmt.Errorf("gdrive: failed to create data part: %w", err)
	}
	if _, err := io.Copy(dataPart, r); err != nil {
		return "", "", fmt.Errorf("gdrive: failed to write data part: %w", err)
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, driveUploadURL+"&fields=id,webViewLink", &buf)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("gdrive: upload request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("gdrive: upload returned %d: %s", resp.StatusCode, body)
	}

	var result driveFile
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("gdrive: failed to parse upload response: %w", err)
	}
	return result.ID, result.WebViewLink, nil
}

// deleteFile permanently deletes a Drive file.
func deleteFile(ctx context.Context, client *http.Client, fileID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, driveFilesURL+"/"+fileID, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gdrive: delete request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gdrive: delete returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

// listFolders lists Drive folders under parentID (or root if empty).
func listFolders(ctx context.Context, client *http.Client, parentID string) ([]*driveFile, error) {
	query := "mimeType = 'application/vnd.google-apps.folder' and trashed = false"
	if parentID != "" {
		query += " and '" + parentID + "' in parents"
	}

	url := driveFilesURL + "?q=" + encodeQuery(query) + "&fields=files(id,name,parents)&orderBy=name"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gdrive: list request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gdrive: list returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Files []*driveFile `json:"files"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("gdrive: failed to parse list response: %w", err)
	}
	return result.Files, nil
}

// createFolder creates a Drive folder.
func createFolder(ctx context.Context, client *http.Client, name, parentID string) (*driveFile, error) {
	meta := driveFile{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
	}
	if parentID != "" {
		meta.Parents = []string{parentID}
	}
	b, _ := json.Marshal(meta)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, driveFilesURL+"?fields=id,name,parents", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gdrive: create folder request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gdrive: create folder returned %d: %s", resp.StatusCode, body)
	}
	var f driveFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func encodeQuery(q string) string {
	// Simple URL encoding of query string.
	return strings.NewReplacer(" ", "+", "'", "%27", "=", "%3D").Replace(q)
}
