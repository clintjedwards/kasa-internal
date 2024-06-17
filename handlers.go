package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

var appVersion = "0.0.dev_000000"

func parseVersion(versionString string) (version, commit string) {
	version, commit, err := strings.Cut(versionString, "_")
	if !err {
		return "", ""
	}

	return
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

type (
	DescribeSystemInfoRequest  struct{}
	DescribeSystemInfoResponse struct {
		Body struct {
			Commit string `json:"commit" example:"e83adcd" doc:"The commit of the current build"`
			Semver string `json:"semver" example:"1.0.0" doc:"The semver version of the current build"`
		}
	}
)

func (apictx *APIContext) registerDescribeSystemInfo(apiDesc huma.API) {
	// Description //
	huma.Register(apiDesc, huma.Operation{
		OperationID: "DescribeSystemInfo",
		Method:      http.MethodGet,
		Path:        "/api/system/info",
		Summary:     "Describe current system information",
		Description: "Return a number of internal meta information about the Gofer server.",
		Tags:        []string{"System"},
		// Handler //
	}, func(_ context.Context, _ *DescribeSystemInfoRequest) (*DescribeSystemInfoResponse, error) {
		version, commit := parseVersion(appVersion)
		resp := &DescribeSystemInfoResponse{}
		resp.Body.Commit = commit
		resp.Body.Semver = version

		return resp, nil
	})
}

type (
	DescribeSystemSummaryRequest  struct{}
	DescribeSystemSummaryResponse struct {
		Body struct{}
	}
)

func (apictx *APIContext) registerDescribeSystemSummary(apiDesc huma.API) {
	// Description //
	huma.Register(apiDesc, huma.Operation{
		OperationID: "DescribeSystemSummary",
		Method:      http.MethodGet,
		Path:        "/api/system/summary",
		Summary:     "Describe various aspects about Gofer's current workloads",
		Description: "A general endpoint to retrieve various metrics about the Gofer service.",
		Tags:        []string{"System"},
		// Handler //
	}, func(_ context.Context, _ *DescribeSystemSummaryRequest) (*DescribeSystemSummaryResponse, error) {
		resp := &DescribeSystemSummaryResponse{}

		return resp, nil
	})
}
