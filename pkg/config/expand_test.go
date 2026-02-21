/*
Copyright © 2026 StackGen, Inc.
*/

package config

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// staticProvider is a test SecretProvider that returns pre-configured values.
type staticProvider struct {
	secrets map[string]string
}

func (s *staticProvider) GetSecret(_ context.Context, name string) (string, error) {
	return s.secrets[name], nil
}

var _ = Describe("expandSecrets", func() {
	var (
		ctx context.Context
		sp  *staticProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		sp = &staticProvider{secrets: map[string]string{
			"FOO":    "bar",
			"SECRET": "s3cret",
			"EMPTY":  "",
		}}
	})

	It("should replace ${VAR} with provider value", func() {
		result := expandSecrets(ctx, sp, "key=${FOO}")
		Expect(result).To(Equal("key=bar"))
	})

	It("should replace $VAR (no braces) with provider value", func() {
		result := expandSecrets(ctx, sp, "key=$FOO")
		Expect(result).To(Equal("key=bar"))
	})

	It("should handle multiple placeholders in one string", func() {
		result := expandSecrets(ctx, sp, "${FOO}:${SECRET}")
		Expect(result).To(Equal("bar:s3cret"))
	})

	It("should replace missing variables with empty string", func() {
		result := expandSecrets(ctx, sp, "val=${MISSING}")
		Expect(result).To(Equal("val="))
	})

	It("should leave text without placeholders unchanged", func() {
		result := expandSecrets(ctx, sp, "no placeholders here")
		Expect(result).To(Equal("no placeholders here"))
	})

	It("should handle empty input", func() {
		result := expandSecrets(ctx, sp, "")
		Expect(result).To(Equal(""))
	})

	It("should handle provider returning empty string", func() {
		result := expandSecrets(ctx, sp, "val=${EMPTY}")
		Expect(result).To(Equal("val="))
	})
})

var _ = Describe("warnUnresolvedSecrets", func() {
	It("should not panic on empty input", func() {
		logger := slog.Default()
		Expect(func() {
			warnUnresolvedSecrets(logger, "test.yaml", "")
		}).ToNot(Panic())
	})

	It("should not panic on input without secret-ish keys", func() {
		logger := slog.Default()
		input := `name = "foo"
description = "bar"`
		Expect(func() {
			warnUnresolvedSecrets(logger, "test.yaml", input)
		}).ToNot(Panic())
	})

	It("should not panic when secret-ish keys have values", func() {
		logger := slog.Default()
		input := `token = "some-value"
api_key = "another-value"`
		Expect(func() {
			warnUnresolvedSecrets(logger, "test.yaml", input)
		}).ToNot(Panic())
	})

	It("should handle YAML-style empty values", func() {
		logger := slog.Default()
		input := `token: ""
api_key: ""`
		Expect(func() {
			warnUnresolvedSecrets(logger, "test.yaml", input)
		}).ToNot(Panic())
	})
})
