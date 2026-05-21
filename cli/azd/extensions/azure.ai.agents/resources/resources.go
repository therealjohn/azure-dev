// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// Package resources owns the static assets embedded in the
// `azure.ai.agents` extension binary.
//
// The embedded `starter` tree is the source-of-truth copy of the
// `Azure-Samples/azd-ai-starter-basic` infrastructure scaffold. The
// extension writes this tree to the user's working directory during
// `azd ai agent init`, so the user never reaches out to GitHub at init
// time.
//
// To regenerate the embedded copy, replace files under `starter/`
// verbatim from the upstream template repo. A `.gitattributes` rule in
// this directory keeps EOLs normalized to LF regardless of contributor
// platform.
package resources

import "embed"

// StarterAzureYaml is the verbatim `azure.yaml` from the starter
// template, written to the project root when scaffolding a new project.
//
//go:embed starter/azure.yaml
var StarterAzureYaml []byte

// StarterInfra is the verbatim `infra/` tree from the starter template.
// A directory match in //go:embed is recursive, so this includes
// starter/infra/main.bicep, starter/infra/main.parameters.json, and
// every module under starter/infra/modules/ with a single directive.
//
//go:embed starter/infra
var StarterInfra embed.FS
