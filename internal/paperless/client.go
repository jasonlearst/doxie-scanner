// Package paperless is a small client for the Paperless-ngx REST API:
// enough to list tags, correspondents and document types, upload a PDF
// with metadata, and follow the consumption task it starts.
package paperless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one Paperless-ngx instance with an API token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a client for the instance at baseURL (e.g.
// "https://paperless.lan"), authenticating with token.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 2 * time.Minute},
	}
}

// BaseURL is the instance's address, for linking to documents.
func (c *Client) BaseURL() string { return c.baseURL }

// Item is a tag, correspondent or document type.
type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Metadata is everything the upload form can choose from.
type Metadata struct {
	Tags           []Item `json:"tags"`
	Correspondents []Item `json:"correspondents"`
	DocumentTypes  []Item `json:"documentTypes"`
}

// Document is what to upload and how to file it. Zero IDs are left for
// Paperless's own matching rules and workflows to decide.
type Document struct {
	PDF           []byte
	Filename      string
	Title         string
	Created       string
	Correspondent int
	DocumentType  int
	Tags          []int
}

// Task states, normalized from Paperless's Celery states.
const (
	TaskQueued     = "queued"
	TaskProcessing = "processing"
	TaskDone       = "done"
	TaskFailed     = "failed"
)

// Task is the progress of one upload's consumption.
type Task struct {
	Status     string
	DocumentID int
	Message    string
}

// ErrUnauthorized means the token was rejected.
var ErrUnauthorized = errors.New("paperless rejected the API token")

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	target := path
	if !strings.HasPrefix(path, "http") {
		target = c.baseURL + path
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("paperless: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, ErrUnauthorized
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("paperless: %s %s: %s %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	resp, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("paperless: decode %s: %w", path, err)
	}
	return nil
}

// Ping checks the instance is reachable and the token works.
func (c *Client) Ping(ctx context.Context) error {
	var ignored any
	return c.getJSON(ctx, "/api/tags/?page_size=1", &ignored)
}

// list follows every page of a list endpoint.
func (c *Client) list(ctx context.Context, path string) ([]Item, error) {
	var all []Item
	next := path + "?page_size=500&ordering=name"
	for next != "" {
		var page struct {
			Next    *string `json:"next"`
			Results []Item  `json:"results"`
		}
		if err := c.getJSON(ctx, next, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Results...)
		next = ""
		if page.Next != nil {
			next = *page.Next
		}
	}
	return all, nil
}

// Metadata fetches every tag, correspondent and document type.
func (c *Client) Metadata(ctx context.Context) (Metadata, error) {
	var m Metadata
	var err error
	if m.Tags, err = c.list(ctx, "/api/tags/"); err != nil {
		return m, err
	}
	if m.Correspondents, err = c.list(ctx, "/api/correspondents/"); err != nil {
		return m, err
	}
	if m.DocumentTypes, err = c.list(ctx, "/api/document_types/"); err != nil {
		return m, err
	}
	return m, nil
}

// Upload hands a PDF to Paperless's consumer and returns the task ID to
// follow with Task.
func (c *Client) Upload(ctx context.Context, doc Document) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("document", doc.Filename)
	if err != nil {
		return "", err
	}
	if _, err := file.Write(doc.PDF); err != nil {
		return "", err
	}
	field := func(name, value string) {
		if value != "" {
			_ = form.WriteField(name, value)
		}
	}
	id := func(n int) string {
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	field("title", doc.Title)
	field("created", doc.Created)
	field("correspondent", id(doc.Correspondent))
	field("document_type", id(doc.DocumentType))
	for _, t := range doc.Tags {
		field("tags", id(t))
	}
	if err := form.Close(); err != nil {
		return "", err
	}

	resp, err := c.do(ctx, http.MethodPost, "/api/documents/post_document/", &body, form.FormDataContentType())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var taskID string
	if json.Unmarshal(raw, &taskID) != nil {
		taskID = strings.Trim(strings.TrimSpace(string(raw)), `"`)
	}
	if taskID == "" {
		return "", errors.New("paperless: upload accepted but returned no task ID")
	}
	return taskID, nil
}

// Task reports how consumption of an upload is going.
func (c *Client) Task(ctx context.Context, taskID string) (Task, error) {
	var tasks []struct {
		Status          string          `json:"status"`
		Result          *string         `json:"result"`
		RelatedDocument json.RawMessage `json:"related_document"`
	}
	if err := c.getJSON(ctx, "/api/tasks/?task_id="+url.QueryEscape(taskID), &tasks); err != nil {
		return Task{}, err
	}
	if len(tasks) == 0 {
		return Task{Status: TaskQueued}, nil
	}
	t := tasks[0]
	out := Task{}
	if t.Result != nil {
		out.Message = *t.Result
	}
	switch t.Status {
	case "SUCCESS":
		out.Status = TaskDone
		out.DocumentID = documentID(t.RelatedDocument)
	case "FAILURE", "REVOKED":
		out.Status = TaskFailed
	case "STARTED":
		out.Status = TaskProcessing
	default:
		out.Status = TaskQueued
	}
	return out, nil
}

// documentID reads related_document, which Paperless has sent both as a
// number and as a string.
func documentID(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		n, _ = strconv.Atoi(s)
	}
	return n
}

// DocumentURL links to a document in the Paperless web UI.
func (c *Client) DocumentURL(id int) string {
	return fmt.Sprintf("%s/documents/%d/details", c.baseURL, id)
}
