package domain

import "time"

type TreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
	Mode string `json:"mode"`
	Size int64  `json:"size,omitempty"`
}

type Commit struct {
	Hash      string    `json:"hash"`
	ShortHash string    `json:"short_hash"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	Email     string    `json:"email"`
	Date      time.Time `json:"date"`
}

type Branch struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	IsHead    bool   `json:"is_head"`
}

type BlobContent struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content"`
	Binary  bool   `json:"binary"`
}
