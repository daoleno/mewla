package server

import (
	"strings"
	"testing"
)

func TestGitDiffPatchInvalidTextIsStillShown(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "invalid.txt", strings.Repeat("\x80", 2048))
	patch, err := (&Server{}).buildGitDiffPatch("", repo, "invalid.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.Sections) != 1 || patch.Sections[0].Scope != "untracked" {
		t.Fatalf("unexpected sections: %+v", patch.Sections)
	}
}

func TestGitDiffPatchKeepsLongLinesAndNoNewlineMarker(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	path := " :[odd]\t->\nname .txt "
	writeGitDiffTestFile(t, repo, path, "old\n")
	runGitDiffTestGit(t, repo, "add", "--", gitLiteralPathspec(path))
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	content := strings.Repeat("界", 30000) + "last-match"
	writeGitDiffTestFile(t, repo, path, content)
	patch, err := (&Server{}).buildGitDiffPatch("", repo, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.Sections) != 1 || patch.Sections[0].Scope != "unstaged" {
		t.Fatalf("unexpected sections: %+v", patch.Sections)
	}
	text := patch.Sections[0].Patch
	if !strings.Contains(text, "\n+"+content+"\n") {
		t.Fatal("the long added line was cut or altered")
	}
	if !strings.HasSuffix(text, "\\ No newline at end of file") {
		t.Fatalf("missing no-newline marker: %q", text[len(text)-40:])
	}
}

func TestGitDiffPatchRenameAndRejectedPath(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "old -> name.txt", "original\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	runGitDiffTestGit(t, repo, "mv", "old -> name.txt", "new\tname.txt")
	writeGitDiffTestFile(t, repo, "new\tname.txt", "working\n")
	files, err := listGitDiffFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].OldPath != "old -> name.txt" || files[0].Status != "renamed" || files[0].Additions != 1 || files[0].Deletions != 1 {
		t.Fatalf("rename stats: %+v", files)
	}
	s := &Server{}
	patch, err := s.buildGitDiffPatch("", repo, files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.Sections) != 2 || patch.Sections[0].Scope != "staged" || patch.Sections[1].Scope != "unstaged" {
		t.Fatalf("unexpected sections: %+v", patch.Sections)
	}
	staged := patch.Sections[0].Patch
	if !strings.Contains(staged, "rename from ") || strings.Contains(staged, "\n+original") || strings.Contains(staged, "\n-original") {
		t.Fatalf("a pure rename must not become an addition and a deletion:\n%s", staged)
	}
	if got := strings.Count(patch.Sections[1].Patch, "\n@@ "); got != 1 {
		t.Fatalf("unstaged hunks = %d:\n%s", got, patch.Sections[1].Patch)
	}
	if _, err = s.buildGitDiffPatch("", repo, "../outside"); err == nil {
		t.Fatal("outside path accepted")
	}
}
