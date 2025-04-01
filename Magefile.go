// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

//go:build mage
// +build mage

// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	// mage:import
	. "github.com/open-edge-platform/cluster-tests/mage" //nolint: revive
)

// To silence compiler's unused import error.
var _ = AsdfPlugins
