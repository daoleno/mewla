package server

import (
	"os"
	"testing"
)

func TestGitDiffUntrackedBinaryDeletedAndEmpty(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "deleted.txt", "old\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	if err := os.Remove(repo + "/deleted.txt"); err != nil {
		t.Fatal(err)
	}
	writeGitDiffTestFile(t, repo, "binary.bin", "hello\x00world")
	writeGitDiffTestFile(t, repo, "empty.txt", "")
	writeGitDiffTestFile(t, repo, "  spaced  ", "one\ntwo")
	files, err := listGitDiffFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("files: %+v", files)
	}
	for _, file := range files {
		patch, err := (&Server{}).buildGitDiffPatch("", repo, file.Path)
		if err != nil {
			t.Fatal(err)
		}
		if len(patch.Sections) == 0 {
			t.Fatalf("missing patch for %q", file.Path)
		}
		switch file.Path {
		case "binary.bin":
			if !file.Binary || file.Additions != 0 {
				t.Fatal(file)
			}
		case "  spaced  ":
			if file.Additions != 2 {
				t.Fatal(file)
			}
		case "deleted.txt":
			if file.Deletions != 1 || file.Status != "deleted" {
				t.Fatal(file)
			}
		}
	}
}

func TestGitDiffUntrackedAttributes(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, ".gitattributes", "*.asset -diff\n*.text diff\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "attributes")
	writeGitDiffTestFile(t, repo, "ascii.asset", "not text\n")
	writeGitDiffTestFile(t, repo, "null.text", "text\x00content\n")
	files, err := listGitDiffFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		out, err := gitCommandOutput(repo, true, "diff", "--no-index", "--numstat", "/dev/null", file.Path)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(file.Path, out)
		if file.Path == "ascii.asset" && (!file.Binary || file.Additions != 0) {
			t.Fatalf("binary attribute ignored: %+v", file)
		}
		if file.Path == "null.text" && (file.Binary || file.Additions != 1) {
			t.Fatalf("text attribute ignored: %+v", file)
		}
	}
}
