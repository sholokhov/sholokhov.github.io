package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/google/go-github/v62/github"
)

type Photo struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Thumb    string `json:"thumb"`
	Caption  string `json:"caption,omitempty"`
	Location string `json:"location,omitempty"`
	Camera   string `json:"camera,omitempty"`
	Date     string `json:"date"`
}

type GitHubSync struct {
	client   *github.Client
	owner    string
	repo     string
	filePath string
	branch   string
}

func NewGitHubSync(cfg GitHubConfig) *GitHubSync {
	client := github.NewClient(nil).WithAuthToken(cfg.Token)

	parts := strings.SplitN(cfg.Repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		log.Fatalf("Invalid github.repo %q: must be \"owner/repo\"", cfg.Repo)
	}
	owner, repo := parts[0], parts[1]

	return &GitHubSync{
		client:   client,
		owner:    owner,
		repo:     repo,
		filePath: cfg.FilePath,
		branch:   cfg.Branch,
	}
}

// AddPhoto reads the current photos.json, appends the new photo, and commits.
func (g *GitHubSync) AddPhoto(ctx context.Context, photo Photo) error {
	// Get current file content and SHA
	file, _, _, err := g.client.Repositories.GetContents(
		ctx, g.owner, g.repo, g.filePath,
		&github.RepositoryContentGetOptions{Ref: g.branch},
	)
	if err != nil {
		return fmt.Errorf("get %s: %w", g.filePath, err)
	}

	content, err := file.GetContent()
	if err != nil {
		return fmt.Errorf("decode content: %w", err)
	}

	var photos []Photo
	if err := json.Unmarshal([]byte(content), &photos); err != nil {
		return fmt.Errorf("parse photos.json: %w", err)
	}

	// Prepend new photo (newest first)
	photos = append([]Photo{photo}, photos...)

	newContent, err := json.MarshalIndent(photos, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal photos: %w", err)
	}
	newContent = append(newContent, '\n')

	_, _, err = g.client.Repositories.UpdateFile(
		ctx, g.owner, g.repo, g.filePath,
		&github.RepositoryContentFileOptions{
			Message: new(fmt.Sprintf("Add photo: %s", photo.ID)),
			Content: newContent,
			SHA:     new(file.GetSHA()),
			Branch:  &g.branch,
		},
	)
	if err != nil {
		return fmt.Errorf("commit %s: %w", g.filePath, err)
	}

	return nil
}
