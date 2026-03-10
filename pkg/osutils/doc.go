// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package osutils provides OS and path helpers used by Genie.
//
// It solves the problem of standardizing paths and environment checks (e.g.
// GenieDir for ~/.genie, home resolution) so that config, audit, DB, and
// other components use the same base directory and behavior across platforms.
// Without it, each package would duplicate home-dir and path logic.
package osutils
