// Package application holds the use-case orchestration: it runs the end-to-end
// "generate release notes" flow by coordinating the domain ports (read commits,
// load issues, render, report progress) without knowing how any of them are
// implemented. The interface (cli) layer wires concrete adapters into it; the
// domain stays unaware that an application layer exists.
package application

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/alexander-danilenko/shipnotes/internal/domain/commit"
	"github.com/alexander-danilenko/shipnotes/internal/domain/notes"
)

// defaultOutputFile is used when the caller does not specify an output path.
const defaultOutputFile = "SHIPNOTES.md"

// Writer persists the rendered notes and reports the final path. The fileoutput
// adapter implements it. It is an application port because writing the document
// is orchestration plumbing, not domain logic.
type Writer interface {
	Write(content, path string) (string, error)
}

// IssueSearcher resolves a JQL query into the keys of the issues it matches,
// used only when the caller passed --jql. The Jira client implements it. Keeping
// it behind a port lets the use case turn the query into the release issue list
// without the application layer depending on the Jira adapter.
type IssueSearcher interface {
	SearchByJQL(ctx context.Context, jql string) ([]string, error)
}

// Service runs the shipnotes use case. Build one with New.
type Service struct {
	repo       commit.Repository
	builder    *notes.Builder
	renderer   notes.Renderer
	writer     Writer
	searcher   IssueSearcher
	coords     notes.Coordinates
	workingDir string
}

// New constructs the Service from its ports and configuration.
func New(
	repo commit.Repository,
	builder *notes.Builder,
	renderer notes.Renderer,
	writer Writer,
	searcher IssueSearcher,
	coords notes.Coordinates,
	workingDir string,
) *Service {
	return &Service{
		repo:       repo,
		builder:    builder,
		renderer:   renderer,
		writer:     writer,
		searcher:   searcher,
		coords:     coords,
		workingDir: workingDir,
	}
}

// Input is everything the use case needs from the caller.
type Input struct {
	CommitHash string
	OutputPath string
	// JQL is the optional --jql query that selects the release issue list. When
	// empty, no issues are pre-selected and the builder summarizes every issue
	// found in the commit range instead.
	JQL string
}

// Result reports what happened, so the caller can print a friendly summary.
type Result struct {
	CommitCount int
	OutputPath  string
}

// Run executes the full flow: validate the commit, read the commits, resolve the
// release issue IDs from the JQL query (when given), build the model, render the
// Markdown, and write it to disk.
func (s *Service) Run(ctx context.Context, in Input) (Result, error) {
	valid, err := s.repo.Validate(ctx, in.CommitHash)
	if err != nil {
		return Result{}, err
	}

	if !valid {
		return Result{}, fmt.Errorf(
			"invalid commit hash: %s. The commit does not exist in the current repository. "+
				"Make sure you're running this command from a git repository", in.CommitHash,
		)
	}

	commits, err := s.repo.Log(ctx, in.CommitHash)
	if err != nil {
		return Result{}, err
	}

	var releaseIssueIDs []string
	if in.JQL != "" {
		releaseIssueIDs, err = s.searcher.SearchByJQL(ctx, in.JQL)
		if err != nil {
			return Result{}, err
		}
	}

	data, err := s.builder.Build(ctx, s.coords, commits, releaseIssueIDs)
	if err != nil {
		return Result{}, err
	}

	markdown, err := s.renderer.Render(data)
	if err != nil {
		return Result{}, err
	}

	finalPath, err := s.writer.Write(markdown, s.resolveOutputPath(in.OutputPath))
	if err != nil {
		return Result{}, err
	}

	return Result{CommitCount: len(commits), OutputPath: finalPath}, nil
}

// resolveOutputPath turns the requested output path into the path we will
// actually write to: absolute paths are used as-is, relative paths are placed
// inside the git repository directory.
func (s *Service) resolveOutputPath(outputPath string) string {
	if outputPath == "" {
		outputPath = defaultOutputFile
	}

	if filepath.IsAbs(outputPath) {
		return outputPath
	}

	return filepath.Join(s.workingDir, outputPath)
}
