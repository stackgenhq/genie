// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package db_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	geniedb "github.com/stackgenhq/genie/pkg/db"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// openTestDB creates a temp SQLite DB with all session tables migrated.
func openTestDB() (*gorm.DB, string) {
	path := GinkgoT().TempDir() + "/test_session.db"
	db, err := geniedb.Open(path)
	Expect(err).NotTo(HaveOccurred())
	Expect(geniedb.AutoMigrate(db)).To(Succeed())
	return db, path
}

var _ = Describe("SessionStore", func() {
	var (
		db    *gorm.DB
		store *geniedb.SessionStore
	)

	BeforeEach(func(ctx context.Context) {
		db, _ = openTestDB()
		store = geniedb.NewSessionStore(ctx, db)
	})

	AfterEach(func(ctx context.Context) {
		geniedb.Close(db)
	})

	// -----------------------------------------------------------------------
	// Session CRUD
	// -----------------------------------------------------------------------

	Describe("CreateSession", func() {
		It("creates a session with initial state", func(ctx context.Context) {
			key := session.Key{AppName: "app", UserID: "u1", SessionID: "s1"}
			state := session.StateMap{"greeting": []byte("hello")}

			sess, err := store.CreateSession(ctx, key, state)
			Expect(err).NotTo(HaveOccurred())
			Expect(sess).NotTo(BeNil())
			Expect(sess.ID).To(Equal("s1"))
		})

		It("auto-generates a session ID when empty", func(ctx context.Context) {
			key := session.Key{AppName: "app", UserID: "u1"}
			sess, err := store.CreateSession(ctx, key, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(sess.ID).NotTo(BeEmpty())
		})
	})

	Describe("GetSession", func() {
		It("returns nil for a non-existent session", func(ctx context.Context) {
			key := session.Key{AppName: "app", UserID: "u1", SessionID: "nope"}
			sess, err := store.GetSession(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(sess).To(BeNil())
		})

		It("retrieves a previously created session", func(ctx context.Context) {
			key := session.Key{AppName: "app", UserID: "u1", SessionID: "s1"}
			_, err := store.CreateSession(ctx, key, nil)
			Expect(err).NotTo(HaveOccurred())

			sess, err := store.GetSession(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(sess).NotTo(BeNil())
			Expect(sess.ID).To(Equal("s1"))
		})
	})

	Describe("ListSessions", func() {
		It("lists all sessions for a user", func(ctx context.Context) {
			userKey := session.UserKey{AppName: "app", UserID: "u1"}
			for _, id := range []string{"a", "b", "c"} {
				key := session.Key{AppName: "app", UserID: "u1", SessionID: id}
				_, err := store.CreateSession(ctx, key, nil)
				Expect(err).NotTo(HaveOccurred())
			}

			sessions, err := store.ListSessions(ctx, userKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(sessions).To(HaveLen(3))
		})
	})

	Describe("DeleteSession", func() {
		It("removes a session and all associated data", func(ctx context.Context) {
			key := session.Key{AppName: "app", UserID: "u1", SessionID: "s1"}
			_, err := store.CreateSession(ctx, key, nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.DeleteSession(ctx, key)).To(Succeed())

			sess, err := store.GetSession(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(sess).To(BeNil())
		})
	})

	// -----------------------------------------------------------------------
	// Event persistence (the core durability guarantee)
	// -----------------------------------------------------------------------

	Describe("AppendEvent", func() {
		It("persists events to the database", func(ctx context.Context) {
			key := session.Key{AppName: "app", UserID: "u1", SessionID: "s1"}
			sess, err := store.CreateSession(ctx, key, nil)
			Expect(err).NotTo(HaveOccurred())

			evt := makeTestEvent("Hello!")
			Expect(store.AppendEvent(ctx, sess, evt)).To(Succeed())

			got, err := store.GetSession(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Events).To(HaveLen(1))
		})

		Context("restart simulation (new SessionStore, same DB)", func() {
			It("restores events from the database on restart", func(ctx context.Context) {
				key := session.Key{AppName: "app", UserID: "u1", SessionID: "s1"}

				// First lifecycle: create + append.
				sess, err := store.CreateSession(ctx, key, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(store.AppendEvent(ctx, sess, makeTestEvent("turn-1"))).To(Succeed())

				// Simulate restart: new store backed by same DB.
				store2 := geniedb.NewSessionStore(ctx, db)
				got, err := store2.GetSession(ctx, key)
				Expect(err).NotTo(HaveOccurred())
				Expect(got).NotTo(BeNil(), "session should survive restart")
				Expect(got.Events).To(HaveLen(1), "events should be restored from DB")
				Expect(got.Events[0].Choices[0].Message.Content).To(Equal("turn-1"))
			})
		})
	})

	// -----------------------------------------------------------------------
	// Event pruning
	// -----------------------------------------------------------------------

	Describe("Event pruning", func() {
		It("keeps only the latest N events in the DB", func(ctx context.Context) {
			limitedStore := geniedb.NewSessionStore(ctx, db, geniedb.WithSessionStoreEventLimit(3))
			key := session.Key{AppName: "app", UserID: "u1", SessionID: "prune"}

			sess, err := limitedStore.CreateSession(ctx, key, nil)
			Expect(err).NotTo(HaveOccurred())

			for i := 0; i < 5; i++ {
				evt := makeTestEvent(string(rune('A' + i)))
				Expect(limitedStore.AppendEvent(ctx, sess, evt)).To(Succeed())
			}

			// Verify via a fresh store (simulates restart).
			freshStore := geniedb.NewSessionStore(ctx, db, geniedb.WithSessionStoreEventLimit(3))
			got, err := freshStore.GetSession(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.Events).To(HaveLen(3), "older events should have been pruned")
		})
	})

	// -----------------------------------------------------------------------
	// State management
	// -----------------------------------------------------------------------

	Describe("App state", func() {
		It("persists and retrieves app-level state", func(ctx context.Context) {
			Expect(store.UpdateAppState(ctx, "myapp", session.StateMap{
				"theme": []byte("dark"),
			})).To(Succeed())

			state, err := store.ListAppStates(ctx, "myapp")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(state["theme"])).To(Equal("dark"))
		})

		It("deletes app state", func(ctx context.Context) {
			Expect(store.UpdateAppState(ctx, "myapp", session.StateMap{
				"foo": []byte("bar"),
			})).To(Succeed())

			Expect(store.DeleteAppState(ctx, "myapp", "foo")).To(Succeed())

			state, err := store.ListAppStates(ctx, "myapp")
			Expect(err).NotTo(HaveOccurred())
			Expect(state).NotTo(HaveKey("foo"))
		})
	})

	Describe("User state", func() {
		It("persists and retrieves user-level state", func(ctx context.Context) {
			userKey := session.UserKey{AppName: "myapp", UserID: "u1"}
			Expect(store.UpdateUserState(ctx, userKey, session.StateMap{
				"lang": []byte("en"),
			})).To(Succeed())

			state, err := store.ListUserStates(ctx, userKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(state["lang"])).To(Equal("en"))
		})

		It("deletes user state", func(ctx context.Context) {
			userKey := session.UserKey{AppName: "myapp", UserID: "u1"}
			Expect(store.UpdateUserState(ctx, userKey, session.StateMap{
				"lang": []byte("en"),
			})).To(Succeed())

			Expect(store.DeleteUserState(ctx, userKey, "lang")).To(Succeed())

			state, err := store.ListUserStates(ctx, userKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(state).NotTo(HaveKey("lang"))
		})
	})

	Describe("Session state", func() {
		It("persists session-level state", func(ctx context.Context) {
			key := session.Key{AppName: "app", UserID: "u1", SessionID: "s1"}
			_, err := store.CreateSession(ctx, key, nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.UpdateSessionState(ctx, key, session.StateMap{
				"draft": []byte("wip content"),
			})).To(Succeed())

			// Verify via fresh store.
			store2 := geniedb.NewSessionStore(ctx, db)
			got, err := store2.GetSession(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			val, ok := got.GetState("draft")
			Expect(ok).To(BeTrue(), "draft state should exist")
			Expect(val).To(Equal([]byte("wip content")))
		})
	})

	// -----------------------------------------------------------------------
	// Builder pattern
	// -----------------------------------------------------------------------

	Describe("Builder", func() {
		It("DefaultSessionStoreBuilder creates a working store", func(ctx context.Context) {
			s := geniedb.DefaultSessionStoreBuilder(ctx, db)
			key := session.Key{AppName: "app", UserID: "u1", SessionID: "bld"}
			_, err := s.CreateSession(ctx, key, nil)
			Expect(err).NotTo(HaveOccurred())

			sess, err := s.GetSession(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(sess).NotTo(BeNil())
		})
	})
})

// makeTestEvent creates a minimal event that passes IsValidContent.
func makeTestEvent(content string) *event.Event {
	return &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: content,
				},
			}},
		},
	}
}
