// Copyright 2017 The WPT Dashboard Project. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package webapp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/web-platform-tests/wpt.fyi/shared"
)

type testRunUIFilter struct {
	PR         *int // GitHub PR to fetch the results for.
	TestRunIDs string
	Products   string
	Labels     string
	SHAs       string
	Aligned    bool
	MaxCount   *int
	Offset     *int
	From       string
	To         string
	Search     string
	// JSON blob of extra (arbitrary) test runs
	TestRuns string
}

type testResultsUIFilter struct {
	testRunUIFilter
	Diff       bool
	DiffFilter string
}

// This handler is responsible for all pages that display test results.
// It fetches the latest TestRun for each browser then renders the HTML
// page with the TestRuns encoded as JSON. The Polymer app picks those up
// and loads the summary files based on each entity's TestRun.ResultsURL.
//
// The browsers initially displayed to the user are defined in browsers.json.
// The JSON property "initially_loaded" is what controls this.
func testResultsHandler(w http.ResponseWriter, r *http.Request) {
	// Redirect legacy paths.
	path := mux.Vars(r)["path"]
	var redir string
	if path == "results" {
		redir = "/results/"
	} else if strings.Index(r.URL.Path, "/results/") != 0 {
		redir = fmt.Sprintf("/results/%s", path)
	}
	if redir != "" {
		params := ""
		if r.URL.RawQuery != "" {
			params = "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, redir+params, http.StatusTemporaryRedirect)
		return
	}

	filter, err := parseTestResultsUIFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := templates.ExecuteTemplate(w, "results.html", filter); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// parseTestResultsUIFilter parses the standard TestRunFilter, as well as the extra
// diff params (diff, before, after).
func parseTestResultsUIFilter(r *http.Request) (filter testResultsUIFilter, err error) {
	q := r.URL.Query()
	testRunFilter, err := shared.ParseTestRunFilterParams(q)
	if err != nil {
		return filter, err
	}
	ctx := shared.NewAppEngineContext(r)
	aeAPI := shared.NewAppEngineAPI(ctx)

	var pr *int
	pr, err = shared.ParsePRParam(q)
	if err != nil {
		return filter, err
	}

	runIDs, err := shared.ParseRunIDsParam(q)
	if err != nil {
		return filter, err
	}

	if len(runIDs) > 0 {
		marshalled, err := json.Marshal(runIDs)
		if err != nil {
			return filter, err
		}
		filter.TestRunIDs = string(marshalled)
	} else {
		if pr == nil && testRunFilter.IsDefaultQuery() {
			experimentalByDefault := aeAPI.IsFeatureEnabled("experimentalByDefault")
			experimentalAlignedExceptEdge := aeAPI.IsFeatureEnabled("experimentalAlignedExceptEdge")
			if experimentalByDefault {
				if experimentalAlignedExceptEdge {
					testRunFilter = testRunFilter.OrAlignedExperimentalRunsExceptEdge()
				} else {
					testRunFilter = testRunFilter.OrExperimentalRuns()
				}
			} else {
				testRunFilter = testRunFilter.OrAlignedStableRuns()
			}
			testRunFilter = testRunFilter.MasterOnly()
		}

		filter.testRunUIFilter = convertTestRunUIFilter(testRunFilter)
		filter.PR = pr
	}

	diff, err := shared.ParseBooleanParam(q, "diff")
	if err != nil {
		return filter, err
	}
	filter.Diff = diff != nil && *diff
	if filter.Diff {
		diffFilter, _, err := shared.ParseDiffFilterParams(q)
		if err != nil {
			return filter, err
		}
		filter.DiffFilter = diffFilter.String()
	}

	var beforeAndAfter shared.ProductSpecs
	if beforeAndAfter, err = shared.ParseBeforeAndAfterParams(q); err != nil {
		return filter, err
	} else if len(beforeAndAfter) > 0 {
		var bytes []byte
		if bytes, err = json.Marshal(beforeAndAfter.Strings()); err != nil {
			return filter, err
		}
		filter.Products = string(bytes)
		filter.Diff = true
	}

	filter.Search = r.URL.Query().Get("q")

	return filter, nil
}

func convertTestRunUIFilter(testRunFilter shared.TestRunFilter) (filter testRunUIFilter) {
	if testRunFilter.Labels != nil {
		data, _ := json.Marshal(testRunFilter.Labels.ToSlice())
		filter.Labels = string(data)
	}
	if !testRunFilter.SHAs.EmptyOrLatest() {
		data, _ := json.Marshal(testRunFilter.SHAs)
		filter.SHAs = string(data)
	}
	if !testRunFilter.IsDefaultProducts() {
		data, _ := json.Marshal(testRunFilter.Products.Strings())
		filter.Products = string(data)
	}
	filter.MaxCount = testRunFilter.MaxCount
	filter.Offset = testRunFilter.Offset
	filter.Aligned = testRunFilter.Aligned != nil && *testRunFilter.Aligned
	if testRunFilter.From != nil {
		filter.From = testRunFilter.From.Format(time.RFC3339)
	}
	if testRunFilter.To != nil {
		filter.To = testRunFilter.To.Format(time.RFC3339)
	}
	return filter
}

func unpackTestRun(base64Run string) (*shared.TestRun, error) {
	decoded, err := base64.URLEncoding.DecodeString(base64Run)
	if err != nil {
		return nil, err
	}
	var run shared.TestRun
	if err := json.Unmarshal([]byte(decoded), &run); err != nil {
		return nil, err
	}
	return &run, nil
}
