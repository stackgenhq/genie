package scm_test

import (
	"testing"

	"github.com/appcd-dev/genie/pkg/tools/scm"
)

func TestNewFactory_GitHub(t *testing.T) {
	svc, err := scm.New(scm.Config{
		Provider: "github",
		Token:    "tok",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewFactory_GitLab(t *testing.T) {
	svc, err := scm.New(scm.Config{
		Provider: "gitlab",
		Token:    "tok",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewFactory_Bitbucket(t *testing.T) {
	svc, err := scm.New(scm.Config{
		Provider: "bitbucket",
		Token:    "tok",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewFactory_UnsupportedProvider(t *testing.T) {
	_, err := scm.New(scm.Config{
		Provider: "unknown",
		Token:    "tok",
	})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestNewFactory_EmptyProvider(t *testing.T) {
	_, err := scm.New(scm.Config{
		Token: "tok",
	})
	if err == nil {
		t.Fatal("expected error for empty provider")
	}
}
