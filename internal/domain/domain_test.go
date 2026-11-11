package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
)

func TestNormalizeNotebookURL(t *testing.T) {
	cases := []struct {
		in      string
		wantID  string
		wantURL string
	}{
		{
			"https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du?usp=sharing#scrollTo=abc",
			"drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du",
			"https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du",
		},
		{
			"https://drive.google.com/file/d/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du/view?usp=drive_link",
			"drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du",
			"https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du",
		},
		{
			"1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du",
			"drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du",
			"https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du",
		},
		{
			"https://colab.research.google.com/github/googlecolab/colabtools/blob/main/notebooks/colab-github-demo.ipynb#scrollTo=123",
			"github/googlecolab/colabtools/blob/main/notebooks/colab-github-demo.ipynb",
			"https://colab.research.google.com/github/googlecolab/colabtools/blob/main/notebooks/colab-github-demo.ipynb",
		},
		{
			"https://colab.research.google.com/gist/octocat/0123456789abcdef/demo.ipynb?hl=en",
			"gist/octocat/0123456789abcdef/demo.ipynb",
			"https://colab.research.google.com/gist/octocat/0123456789abcdef/demo.ipynb",
		},
	}

	for _, tc := range cases {
		gotID, gotURL, err := domain.NormalizeNotebookURL(tc.in)
		if err != nil || gotID != tc.wantID || gotURL != tc.wantURL {
			t.Fatalf("NormalizeNotebookURL(%q) = (%q, %q, %v), want (%q, %q, nil)", tc.in, gotID, gotURL, err, tc.wantID, tc.wantURL)
		}
	}

	if _, _, err := domain.NormalizeNotebookURL("   "); err == nil {
		t.Fatal("expected error for empty notebook URL")
	}
	if _, _, err := domain.NormalizeNotebookURL("https://example.com/not-a-notebook"); err == nil {
		t.Fatal("expected error for unsupported domain")
	}
}

func TestGenerateTokenAndPrefix(t *testing.T) {
	ptok, err := domain.GenerateToken(domain.TokenPrefixProject)
	if err != nil {
		t.Fatalf("GenerateToken(glp_): %v", err)
	}
	if !strings.HasPrefix(ptok, "glp_") || len(ptok) != 4+32 {
		t.Fatalf("unexpected project token format: %q (len=%d)", ptok, len(ptok))
	}

	stok, err := domain.GenerateToken(domain.TokenPrefixSession)
	if err != nil {
		t.Fatalf("GenerateToken(gls_): %v", err)
	}
	if !strings.HasPrefix(stok, "gls_") || len(stok) != 4+32 {
		t.Fatalf("unexpected session token format: %q (len=%d)", stok, len(stok))
	}

	if ptok == stok {
		t.Fatal("tokens must be unique")
	}

	prefix := domain.MaskTokenPrefix(ptok)
	if !strings.HasPrefix(prefix, "glp_") || !strings.HasSuffix(prefix, "...") || strings.Contains(prefix, ptok[12:]) {
		t.Fatalf("unexpected masked token prefix: %q", prefix)
	}
}

func TestAppErrorAndEnums(t *testing.T) {
	err := domain.NewAppError(domain.CodeProjectUnlocked, "project has no lock", "Lock an account session in GoLab UI", 409)
	if err.Code != domain.CodeProjectUnlocked || err.HTTPStatus != 409 {
		t.Fatalf("unexpected AppError: %+v", err)
	}
	if !strings.Contains(err.Error(), domain.CodeProjectUnlocked) {
		t.Fatalf("Error() should include code: %q", err.Error())
	}

	if !domain.PermissionOwner.CanWrite() || !domain.PermissionEditor.CanWrite() {
		t.Fatal("owner and editor must have write capability")
	}
	if domain.PermissionViewOnly.CanWrite() || domain.PermissionCommenter.CanWrite() || domain.PermissionDenied.CanWrite() {
		t.Fatal("view_only/commenter/denied must NOT have write capability")
	}

	now := time.Now().UTC()
	proj := domain.Project{
		ID:             "p1",
		Name:           "Demo",
		NotebookID:     "drive/123",
		NotebookURL:    "https://colab.research.google.com/drive/123",
		OwnerProfileID: "default",
		ProjectToken:   "glp_test",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if proj.NotebookID != "drive/123" {
		t.Fatalf("unexpected project: %+v", proj)
	}

	sess := domain.Session{
		ID:              "s1",
		ProjectID:       "p1",
		ProfileID:       "default",
		Name:            "Account 1",
		SessionToken:    "gls_test",
		Permission:      domain.PermissionOwner,
		ConnectionState: domain.StateIdle,
		CreatedAt:       now,
	}
	if sess.ConnectionState != domain.StateIdle || !sess.Permission.CanWrite() {
		t.Fatalf("unexpected session: %+v", sess)
	}
}
