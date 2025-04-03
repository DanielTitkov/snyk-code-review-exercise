package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/snyk/snyk-code-review-exercise/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// review: it's also useful to have tests for different edge cases:
// package not found, version not found, invalid version, etc.
func TestPackageHandler(t *testing.T) {
	handler := api.New()
	server := httptest.NewServer(handler)
	defer server.Close()

	// review: the test does not pass. This is due to the fact that the package version
	// in the remote source apparently was updated and does not match the fixture anymore.
	// It's better not to rely on external sources for tests, as they can change over time
	// or become unavailable.
	// The solution would be to use a mock server to simulate the remote source, otherwise
	// the test might randomly fail.
	resp, err := server.Client().Get(server.URL + "/package/react/16.13.0")
	require.Nil(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.Nil(t, err)

	var data api.NpmPackageVersion
	err = json.Unmarshal(body, &data)
	require.Nil(t, err)

	assert.Equal(t, "react", data.Name)
	assert.Equal(t, "16.13.0", data.Version)

	fixture, err := os.Open(filepath.Join("testdata", "react-16.13.0.json"))
	require.Nil(t, err)
	var fixtureObj api.NpmPackageVersion
	require.Nil(t, json.NewDecoder(fixture).Decode(&fixtureObj))

	assert.Equal(t, fixtureObj, data)
}
