// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore_test

import (
	"context"
	"time"

	"github.com/markbates/goth/providers/faux"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/credstore"
	"github.com/stackgenhq/genie/pkg/credstore/credstorefakes"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/security/securityfakes"
)

var _ = Describe("MemoryBackend", func() {
	var (
		backend *credstore.MemoryBackend
		ctx     context.Context
	)

	BeforeEach(func() {
		backend = credstore.NewMemoryBackend()
		ctx = context.Background()
	})

	Describe("round-trip", func() {
		It("should return ErrNoToken before any Put", func() {
			_, err := backend.Get(ctx, credstore.BackendGetRequest{UserID: "user-1", ServiceName: "github"})
			Expect(err).To(HaveOccurred())
		})

		It("should store and retrieve a token", func() {
			tok := &credstore.Token{AccessToken: "abc", TokenType: "Bearer"}
			Expect(backend.Put(ctx, credstore.BackendPutRequest{
				UserID: "user-1", ServiceName: "github", Token: tok,
			})).To(Succeed())

			got, err := backend.Get(ctx, credstore.BackendGetRequest{UserID: "user-1", ServiceName: "github"})
			Expect(err).NotTo(HaveOccurred())
			Expect(got.AccessToken).To(Equal("abc"))
		})

		It("should return ErrNoToken after Delete", func() {
			tok := &credstore.Token{AccessToken: "abc"}
			_ = backend.Put(ctx, credstore.BackendPutRequest{
				UserID: "user-1", ServiceName: "github", Token: tok,
			})
			Expect(backend.Delete(ctx, credstore.BackendDeleteRequest{
				UserID: "user-1", ServiceName: "github",
			})).To(Succeed())

			_, err := backend.Get(ctx, credstore.BackendGetRequest{UserID: "user-1", ServiceName: "github"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("user isolation", func() {
		It("should isolate tokens between different users", func() {
			_ = backend.Put(ctx, credstore.BackendPutRequest{
				UserID: "alice", ServiceName: "github",
				Token: &credstore.Token{AccessToken: "alice-token"},
			})
			_ = backend.Put(ctx, credstore.BackendPutRequest{
				UserID: "bob", ServiceName: "github",
				Token: &credstore.Token{AccessToken: "bob-token"},
			})

			alice, err := backend.Get(ctx, credstore.BackendGetRequest{UserID: "alice", ServiceName: "github"})
			Expect(err).NotTo(HaveOccurred())
			Expect(alice.AccessToken).To(Equal("alice-token"))

			bob, err := backend.Get(ctx, credstore.BackendGetRequest{UserID: "bob", ServiceName: "github"})
			Expect(err).NotTo(HaveOccurred())
			Expect(bob.AccessToken).To(Equal("bob-token"))
		})
	})

	Describe("context cancellation", func() {
		It("should return context error when context is cancelled", func() {
			cancelledCtx, cancel := context.WithCancel(ctx)
			cancel()

			_, err := backend.Get(cancelledCtx, credstore.BackendGetRequest{UserID: "u", ServiceName: "s"})
			Expect(err).To(MatchError(context.Canceled))
		})
	})
})

var _ = Describe("StaticStore", func() {
	var (
		sp    *securityfakes.FakeSecretProvider
		store credstore.Store
	)

	BeforeEach(func() {
		sp = &securityfakes.FakeSecretProvider{}
	})

	Describe("GetToken", func() {
		Context("when SecretProvider has a token", func() {
			BeforeEach(func() {
				sp.GetSecretReturns("my-pat-token", nil)
				store = credstore.NewStaticStore(credstore.NewStaticStoreRequest{
					ServiceName: "github",
					Provider:    sp,
					SecretName:  "GITHUB_TOKEN",
				})
			})

			It("should return a Bearer token", func() {
				tok, err := store.GetToken(context.Background())
				Expect(err).NotTo(HaveOccurred())
				Expect(tok.AccessToken).To(Equal("my-pat-token"))
				Expect(tok.TokenType).To(Equal("Bearer"))
			})

			It("should call SecretProvider with the correct secret name", func() {
				_, _ = store.GetToken(context.Background())
				Expect(sp.GetSecretCallCount()).To(Equal(1))
				_, req := sp.GetSecretArgsForCall(0)
				Expect(req.Name).To(Equal("GITHUB_TOKEN"))
			})
		})

		Context("when SecretProvider returns empty", func() {
			BeforeEach(func() {
				sp.GetSecretReturns("", nil)
				store = credstore.NewStaticStore(credstore.NewStaticStoreRequest{
					ServiceName: "github",
					Provider:    sp,
					SecretName:  "GITHUB_TOKEN",
				})
			})

			It("should return ErrNoToken", func() {
				_, err := store.GetToken(context.Background())
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("SaveToken and DeleteToken", func() {
		BeforeEach(func() {
			store = credstore.NewStaticStore(credstore.NewStaticStoreRequest{
				ServiceName: "test", Provider: sp, SecretName: "X",
			})
		})

		It("should be no-ops for static stores", func() {
			Expect(store.SaveToken(context.Background(), &credstore.Token{})).To(Succeed())
			Expect(store.DeleteToken(context.Background())).To(Succeed())
		})
	})

	Describe("ServiceName", func() {
		It("should return the configured service name", func() {
			store = credstore.NewStaticStore(credstore.NewStaticStoreRequest{
				ServiceName: "github", Provider: sp, SecretName: "X",
			})
			Expect(store.ServiceName()).To(Equal("github"))
		})
	})
})

var _ = Describe("OAuthStore", func() {
	var (
		backend *credstore.MemoryBackend
		store   credstore.Store
	)

	BeforeEach(func() {
		backend = credstore.NewMemoryBackend()
	})

	Describe("GetToken", func() {
		Context("when a valid token exists in the backend", func() {
			It("should return the stored token", func() {
				ctx := contextWithUser("user-1")
				_ = backend.Put(ctx, credstore.BackendPutRequest{
					UserID: "user-1", ServiceName: "jira",
					Token: &credstore.Token{AccessToken: "valid-tok", TokenType: "Bearer"},
				})

				store = credstore.NewOAuthStore(credstore.NewOAuthStoreRequest{
					ServiceName: "jira",
					Backend:     backend,
					Provider:    &faux.Provider{},
					Pending:     credstore.NewPendingAuthStore(),
				})

				tok, err := store.GetToken(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(tok.AccessToken).To(Equal("valid-tok"))
			})
		})

		Context("when no token exists", func() {
			BeforeEach(func() {
				store = credstore.NewOAuthStore(credstore.NewOAuthStoreRequest{
					ServiceName: "jira",
					Backend:     credstore.NewMemoryBackend(),
					Provider:    &faux.Provider{},
					Pending:     credstore.NewPendingAuthStore(),
				})
			})

			It("should return an AuthRequiredError", func() {
				ctx := contextWithUser("user-2")
				_, err := store.GetToken(ctx)
				Expect(err).To(HaveOccurred())
				Expect(credstore.IsAuthRequiredError(err)).To(BeTrue())
			})

			It("should include an auth URL from the goth provider", func() {
				ctx := contextWithUser("user-2")
				_, err := store.GetToken(ctx)
				url := credstore.GetAuthURL(err)
				Expect(url).NotTo(BeEmpty())
				Expect(url).To(ContainSubstring("example.com/auth"))
			})
		})
	})

	Describe("SaveToken and DeleteToken", func() {
		BeforeEach(func() {
			store = credstore.NewOAuthStore(credstore.NewOAuthStoreRequest{
				ServiceName: "jira",
				Backend:     backend,
				Provider:    &faux.Provider{},
				Pending:     credstore.NewPendingAuthStore(),
			})
		})

		It("should persist and retrieve a saved token", func() {
			ctx := contextWithUser("user-3")
			tok := &credstore.Token{AccessToken: "saved-tok", TokenType: "Bearer"}
			Expect(store.SaveToken(ctx, tok)).To(Succeed())

			got, err := store.GetToken(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.AccessToken).To(Equal("saved-tok"))
		})

		It("should return AuthRequiredError after delete", func() {
			ctx := contextWithUser("user-3")
			_ = store.SaveToken(ctx, &credstore.Token{AccessToken: "tok"})
			Expect(store.DeleteToken(ctx)).To(Succeed())

			_, err := store.GetToken(ctx)
			Expect(credstore.IsAuthRequiredError(err)).To(BeTrue())
		})
	})
})

var _ = Describe("PendingAuthStore", func() {
	var store *credstore.PendingAuthStore

	BeforeEach(func() {
		store = credstore.NewPendingAuthStore()
	})

	Describe("Store and Load", func() {
		It("should store and retrieve an entry by state", func() {
			store.Store("state-abc", credstore.PendingAuth{
				UserID:         "user-1",
				ServiceName:    "jira",
				SessionMarshal: `{"ID":"test-session"}`,
				ExpiresAt:      time.Now().Add(5 * time.Minute),
			})

			got, ok := store.Load("state-abc")
			Expect(ok).To(BeTrue())
			Expect(got.UserID).To(Equal("user-1"))
			Expect(got.SessionMarshal).To(Equal(`{"ID":"test-session"}`))
		})

		It("should consume the entry on Load (one-time use)", func() {
			store.Store("state-abc", credstore.PendingAuth{
				UserID:    "user-1",
				ExpiresAt: time.Now().Add(5 * time.Minute),
			})

			_, ok := store.Load("state-abc")
			Expect(ok).To(BeTrue())

			_, ok = store.Load("state-abc")
			Expect(ok).To(BeFalse(), "second Load should return false (consumed)")
		})

		It("should not return expired entries", func() {
			store.Store("expired-state", credstore.PendingAuth{
				UserID:    "user-1",
				ExpiresAt: time.Now().Add(-1 * time.Second),
			})

			_, ok := store.Load("expired-state")
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Cleanup", func() {
		It("should remove expired entries and keep valid ones", func() {
			store.Store("valid", credstore.PendingAuth{
				UserID:    "u1",
				ExpiresAt: time.Now().Add(10 * time.Minute),
			})
			store.Store("expired", credstore.PendingAuth{
				UserID:    "u2",
				ExpiresAt: time.Now().Add(-1 * time.Second),
			})

			store.Cleanup()

			_, ok := store.Load("valid")
			Expect(ok).To(BeTrue())
		})
	})
})

var _ = Describe("Error Types", func() {
	Describe("AuthRequiredError", func() {
		It("should be detected by IsAuthRequiredError", func() {
			err := &credstore.AuthRequiredError{AuthURL: "https://example.com/auth", ServiceName: "jira"}
			Expect(credstore.IsAuthRequiredError(err)).To(BeTrue())
		})

		It("should return the AuthURL via GetAuthURL", func() {
			err := &credstore.AuthRequiredError{AuthURL: "https://example.com/auth"}
			Expect(credstore.GetAuthURL(err)).To(Equal("https://example.com/auth"))
		})
	})

	Describe("ErrNoToken", func() {
		It("should not match IsAuthRequiredError", func() {
			Expect(credstore.IsAuthRequiredError(credstore.ErrNoToken)).To(BeFalse())
		})

		It("should return empty from GetAuthURL", func() {
			Expect(credstore.GetAuthURL(credstore.ErrNoToken)).To(BeEmpty())
		})
	})
})

var _ = Describe("Manager", func() {
	Describe("RegisterStatic and StoreFor", func() {
		It("should register and retrieve a static store", func() {
			mgr := credstore.NewManager(credstore.NewManagerRequest{})
			fakeStore := &credstorefakes.FakeStore{}
			fakeStore.ServiceNameReturns("github")
			mgr.RegisterStatic(fakeStore)

			got := mgr.StoreFor("github")
			Expect(got).NotTo(BeNil())
			Expect(got.ServiceName()).To(Equal("github"))
		})

		It("should return nil for unknown services", func() {
			mgr := credstore.NewManager(credstore.NewManagerRequest{})
			Expect(mgr.StoreFor("unknown")).To(BeNil())
		})
	})

	Describe("RegisterOAuth", func() {
		It("should register and retrieve an OAuth store", func() {
			mgr := credstore.NewManager(credstore.NewManagerRequest{
				Backend: credstore.NewMemoryBackend(),
			})

			store := mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
				ServiceName: "jira",
				Provider:    &faux.Provider{},
			})

			Expect(store.ServiceName()).To(Equal("jira"))
			Expect(mgr.StoreFor("jira")).NotTo(BeNil())
		})
	})
})

var _ = Describe("Token", func() {
	Describe("IsExpired", func() {
		It("should return false when no expiry is set", func() {
			tok := credstore.Token{AccessToken: "x"}
			Expect(tok.IsExpired()).To(BeFalse())
		})

		It("should return false when expiry is in the future", func() {
			tok := credstore.Token{ExpiresAt: time.Now().Add(time.Hour)}
			Expect(tok.IsExpired()).To(BeFalse())
		})

		It("should return true when expiry is in the past", func() {
			tok := credstore.Token{ExpiresAt: time.Now().Add(-time.Hour)}
			Expect(tok.IsExpired()).To(BeTrue())
		})
	})
})

// --- helpers ---

func contextWithUser(userID string) context.Context {
	return messenger.WithMessageOrigin(context.Background(), messenger.MessageOrigin{
		Platform: "test",
		Sender:   messenger.Sender{ID: userID},
		Channel:  messenger.Channel{ID: "test-channel"},
	})
}
