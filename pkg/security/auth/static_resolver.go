// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
)

// staticResolver maps hardcoded user IDs (like emails) to specific
// enterprise roles, groups, and departments.
type staticResolver struct {
	// users maps the lowercase sender ID to their resolved metadata.
	users map[string]EnterpriseUser
}

// newStaticResolver parses a configuration map into a staticResolver.
// Keys are user IDs (e.g. "john@acme.com").
// Values are formatted strings: "role:admin,groups:infra|dev,dept:engineering"
func newStaticResolver(cfg map[string]string) *staticResolver {
	users := make(map[string]EnterpriseUser, len(cfg))
	for userID, val := range cfg {
		userID = strings.ToLower(strings.TrimSpace(userID))
		if userID == "" {
			continue
		}

		eu := EnterpriseUser{}
		parts := strings.Split(val, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			kv := strings.SplitN(part, ":", 2)
			if len(kv) != 2 {
				continue
			}
			k := strings.ToLower(strings.TrimSpace(kv[0]))
			v := strings.TrimSpace(kv[1])

			switch k {
			case "role":
				eu.Role = v
			case "dept", "department":
				eu.Department = v
			case "groups":
				groupList := strings.Split(v, "|")
				var validGroups []string
				for i := range groupList {
					g := strings.TrimSpace(groupList[i])
					if g != "" {
						validGroups = append(validGroups, g)
					}
				}
				eu.Groups = validGroups
			}
		}
		users[userID] = eu
	}
	return &staticResolver{users: users}
}

// Resolve looks up the incoming Sender.ID in the hardcoded map.
func (s *staticResolver) Resolve(ctx context.Context, req ResolveRequest) (EnterpriseUser, error) {
	logr := logger.GetLogger(ctx).With("fn", "staticResolver.Resolve", "user_id", req.Sender.ID)

	userID := strings.ToLower(strings.TrimSpace(req.Sender.ID))
	if eu, ok := s.users[userID]; ok {
		logr.Debug("resolved static identity", "role", eu.Role)

		// Copy metadata to the incoming sender
		eu.Sender = req.Sender
		eu.Role = s.users[userID].Role

		return eu, nil
	}

	return EnterpriseUser{Sender: req.Sender}, nil
}
