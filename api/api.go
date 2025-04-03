package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/gorilla/mux"
)

// idea: clearly the code is heavily simplified (as an exercise),
// but here are some general considerations for the sake of completeness:
// - separate API, business logic and external services access layers to different packages
// - use interfaces to allow for different implementations and data sources
// - use more complex routing to allow adding other languages and registries in the future
// smth like: /{api_version}/{language}/{package}/{version} with an optional registry query param
// if adding something except for js/npm might ever be needed
// - use cache to reduce the number of requests to external sources
// - use a more sophisticated error handling strategy (e.g. custom error types, logging, etc.)
// - use middleware for authentication, throttling, error handling, etc.
// - use a production-grade logger like zap or logrus instead of println (or at leasts standard log)

// idea: it may be a good idea to use a more versatile web framework like gin or echo
// that would allow for better error handling and middleware support and also make handlers more concise
// (error handling, request validation, response marshalling, etc.)
func New() http.Handler {
	router := mux.NewRouter()
	router.Handle("/package/{package}/{version}", http.HandlerFunc(packageHandler))
	return router
}

// idea: as this is a part of the business logic, it might be a good idea to move it to a separate package
// to keep the API handler code clean and focused on routing and request handling.
// We might also want to have a separate models for API requests and responses. That will make code more
// verbose, but may come in handy if we need to change the API in the future and keep it independent from
// the internal representation of the data (or have another type of API, e.g. gRPC).
type npmPackageMetaResponse struct {
	Versions map[string]npmPackageResponse `json:"versions"`
}

type npmPackageResponse struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

type NpmPackageVersion struct {
	Name         string                        `json:"name"`
	Version      string                        `json:"version"`
	Dependencies map[string]*NpmPackageVersion `json:"dependencies"`
}

func packageHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pkgName := vars["package"]
	pkgVersion := vars["version"]

	// review: we can further simplify the handler by using a constructor for NpmPackageVersion,
	// this way it will be more reusable and we won't have to create map in the handler code
	// (especially given the same code is used in the resolveDependencies function).
	// That may come especially handy if in the future we need to add more logic to the struct initialization.
	rootPkg := &NpmPackageVersion{Name: pkgName, Dependencies: map[string]*NpmPackageVersion{}}
	if err := resolveDependencies(rootPkg, pkgVersion); err != nil {
		println(err.Error())
		w.WriteHeader(500)
		return
	}

	// review: it might be a useful idea to move indentation options to a config or have it as an optional
	// request parameter to allow the client to choose the format they prefer,
	// as some clients might prefer a minified version for performance reasons.
	stringified, err := json.MarshalIndent(rootPkg, "", "  ")
	if err != nil {
		println(err.Error())
		// review: it's better to add a meaningful error message for the user to understand what went wrong
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	// Ignoring ResponseWriter errors
	// review: it's better not to ignore any errors, better to log them
	_, _ = w.Write(stringified)
}

// review: it's great that we moved the logic to a separate function, well done!
func resolveDependencies(pkg *NpmPackageVersion, versionConstraint string) error {
	// review: as we are working with external source we cannot completely disregard the possibility
	// of circular dependencies which will lead to infinite recursion. So it will be useful to
	// - limit the depth of the recursion
	// - keep track of the visited packages/versions

	// log.Println("Resolving dependencies for", pkg.Name, versionConstraint)
	pkgMeta, err := fetchPackageMeta(pkg.Name)
	if err != nil {
		// review: it might useful to wrap the error with more context
		// to understand where it happened and what was the input
		// e.g. fmt.Errorf("failed to fetch package meta for %s: %w", pkg.Name, err)
		// (same is applicable for other errors in the code)
		return err
	}
	concreteVersion, err := highestCompatibleVersion(versionConstraint, pkgMeta)
	if err != nil {
		return err
	}
	pkg.Version = concreteVersion

	// review: it might be a good idea to log the version resolution process
	// to help with debugging and understanding the dependency tree.
	// log.Printf("Resolved %s@%s to %s", pkg.Name, versionConstraint, concreteVersion)
	npmPkg, err := fetchPackage(pkg.Name, pkg.Version)
	if err != nil {
		return err
	}
	// review: this simple recursive process might be not ideal for the task. There are some considerations:
	// - in case of large number of dependencies it might take long time to resolve all of them
	// we might want to process some of them in parallel
	// - we don't control the frequency of the requests to the external source
	// and we might hit the rate limit. We might want to add some kind of throttling and timing
	// - if we fail to resolve one dependency of the tree we probably don't want to fail the whole process
	// (check "serverless" example as that package has a lot of dependencies while react is relatively small)
	// - as already mentioned before ,we might want to add some kind of caching to avoid
	// fetching the same package multiple times. At least sync/singleflight might be a good idea and for
	// production level we would want to use a proper caching solution.
	for dependencyName, dependencyVersionConstraint := range npmPkg.Dependencies {
		dep := &NpmPackageVersion{Name: dependencyName, Dependencies: map[string]*NpmPackageVersion{}}
		pkg.Dependencies[dependencyName] = dep
		if err := resolveDependencies(dep, dependencyVersionConstraint); err != nil {
			return err
		}
	}
	return nil
}

func highestCompatibleVersion(constraintStr string, versions *npmPackageMetaResponse) (string, error) {
	// review: some actual packages don't match the semver spec, so we might want to
	// think of a way to process them as well. E.g. "npm:strip-ansi@^6.0.1" leads to failure of this check.
	constraint, err := semver.NewConstraint(constraintStr)
	if err != nil {
		return "", err
	}
	filtered := filterCompatibleVersions(constraint, versions)
	sort.Sort(filtered)
	if len(filtered) == 0 {
		return "", errors.New("no compatible versions found")
	}
	return filtered[len(filtered)-1].String(), nil
}

func filterCompatibleVersions(constraint *semver.Constraints, pkgMeta *npmPackageMetaResponse) semver.Collection {
	var compatible semver.Collection
	for version := range pkgMeta.Versions {
		semVer, err := semver.NewVersion(version)
		if err != nil {
			// review: we might want to log the error here, as it might be useful for debugging
			// log.Println("Error parsing version:", err)
			continue
		}
		if constraint.Check(semVer) {
			compatible = append(compatible, semVer)
		}
	}
	return compatible
}

func fetchPackage(name, version string) (*npmPackageResponse, error) {
	// idea: this is out of the scope of the PR, but we might want to consider using an
	// interface here to allow for different implementations of fetching logic like local files
	// or different registries. Also, it will allow for better encapsulation of the fetch logic.
	// Also it better to move the URL to a constant, so we can change it in one place if needed.
	resp, err := http.Get(fmt.Sprintf("https://registry.npmjs.org/%s/%s", name, version))
	// review: it might be useful to check response status code here to distinguish between
	// different types of errors (e.g. 404, 500, etc.) and handle them accordingly.
	// otherwise for example we can get a 404 response and still try to unmarshal it
	// if resp.StatusCode != http.StatusOK {
	// 	return nil, fmt.Errorf("unexpected status code %d fetching %s@%s", resp.StatusCode, name, version)
	// }
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageResponse
	// review: we probably don't want to ignore the error here, as it might lead to unexpected behavior
	// and complicate debugging. It's better to handle the error properly and return it to the caller.
	_ = json.Unmarshal(body, &parsed)
	return &parsed, nil
}

func fetchPackageMeta(p string) (*npmPackageMetaResponse, error) {
	// idea: same as above.
	resp, err := http.Get(fmt.Sprintf("https://registry.npmjs.org/%s", p))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageMetaResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, err
	}

	return &parsed, nil
}
