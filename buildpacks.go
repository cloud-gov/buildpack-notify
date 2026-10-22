package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"golang.org/x/mod/semver"
)

var (
	buildpackZipFileRegex = regexp.MustCompile(`^([a-z]+(-)*)+(_|-)buildpack(_|-)cflinuxfs[0-9]{1}(_|-)(v[0-9]+\.[0-9]+\.[0-9]+).zip$`)
	buildpackGitRepoRegex = regexp.MustCompile(`^https://github\.com/cloudfoundry/([a-z]+-buildpack)\.git#(v[0-9]+\.[0-9]+(\.[0-9]+)?)$`)
)

type buildpackReleaseInfo struct {
	BuildpackName    string
	BuildpackVersion string
	BuildpackURL     string
}

type buildpackSource int

const (
	BUILDPACK_SYSTEM buildpackSource = iota
	BUILDPACK_GIT_PINNED
)

func (s buildpackSource) String() string {
	switch s {
	case BUILDPACK_SYSTEM:
		return "system"
	case BUILDPACK_GIT_PINNED:
		return "pinned git repo"
	default:
		return "unknown"
	}
}

func isBuildpackFilenamePinnedToGitRepo(buildpackFilename string) bool {
	return buildpackGitRepoRegex.Match([]byte(buildpackFilename))
}

func getPinnedGitRepoBuildpackName(buildpackFilename string) string {
	match := buildpackGitRepoRegex.FindStringSubmatch(buildpackFilename)
	if len(match) == 0 {
		return ""
	}
	return strings.ReplaceAll(match[len(match)-3], "-", "_")
}

func getPinnedGitRepoBuildpackVersion(buildpackFilename string) string {
	match := buildpackGitRepoRegex.FindStringSubmatch(buildpackFilename)
	if len(match) == 0 {
		return ""
	}
	return match[len(match)-2]
}

func getBuildpackReleaseURL(buildpackName string) string {
	// Returns the release notes page for a given buildpack; if the buildpack is
	// not found, returns an empty string.

	// Map of all supported system buildpack releases in Cloud Foundry.
	buildpackReleaseURLs := map[string]string{
		"staticfile_buildpack":  "https://github.com/cloudfoundry/staticfile-buildpack/releases",
		"java_buildpack":        "https://github.com/cloudfoundry/java-buildpack/releases",
		"ruby_buildpack":        "https://github.com/cloudfoundry/ruby-buildpack/releases",
		"dotnet_core_buildpack": "https://github.com/cloudfoundry/dotnet-core-buildpack/releases",
		"nodejs_buildpack":      "https://github.com/cloudfoundry/nodejs-buildpack/releases",
		"go_buildpack":          "https://github.com/cloudfoundry/go-buildpack/releases",
		"python_buildpack":      "https://github.com/cloudfoundry/python-buildpack/releases",
		"php_buildpack":         "https://github.com/cloudfoundry/php-buildpack/releases",
		"binary_buildpack":      "https://github.com/cloudfoundry/binary-buildpack/releases",
		"nginx_buildpack":       "https://github.com/cloudfoundry/nginx-buildpack/releases",
		"r_buildpack":           "https://github.com/cloudfoundry/r-buildpack/releases",
	}

	// Note that for a specific release, you'll need to append
	// /tag/<version_number> at the end, e.g.,
	// https://github.com/cloudfoundry/python-buildpack/releases/tag/v1.7.45
	// for the Python buildpack.

	if buildpackReleaseURL, ok := buildpackReleaseURLs[buildpackName]; ok {
		return buildpackReleaseURL
	}

	return ""
}

func parseSystemBuildpackVersion(buildpackFileName string) (string, error) {
	// Takes a buildpack file name and parses out the version number from it.
	// Buildpack filenames currently look like this: python_buildpack-cflinuxfs3-v1.7.43.zip
	// "v1.7.43" is the version in this case.

	if buildpackZipFileRegex.Match([]byte(buildpackFileName)) {
		match := buildpackZipFileRegex.FindStringSubmatch(buildpackFileName)
		if len(match) == 0 {
			return "", errors.New("could not parse version")
		}
		version := match[len(match)-1]
		if !semver.IsValid(version) {
			return "", fmt.Errorf("%s is not a valid semantic version", version)
		}
		return version, nil
	}

	return "", fmt.Errorf("unsupported filename: %s", buildpackFileName)
}

func getBuildpackVersionURL(buildpackReleaseURL string, buildpackVersion string) string {
	// Takes a buildpack version and appends it to a URL to create a specific
	// release URL.  If the version isn't correct, fall back to the main
	// releases URL.
	buildpackVersionURL := buildpackReleaseURL
	buildpackVersionPath := "/tag/"

	// Check to make sure that the buildpackVersion matches the format of
	// vX.Y[.Z], e.g.: v1.7.43 or v1.6
	if semver.IsValid(buildpackVersion) {
		buildpackVersionURL = buildpackReleaseURL + buildpackVersionPath + buildpackVersion
	}

	return buildpackVersionURL
}

func deduplicateBuildpacks(allBuildpacks []buildpackReleaseInfo) []buildpackReleaseInfo {
	keys := make(map[buildpackReleaseInfo]bool)
	var deduplicated []buildpackReleaseInfo
	for _, entry := range allBuildpacks {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			deduplicated = append(deduplicated, entry)
		}
	}
	return deduplicated
}

// isBuildpackSupported checks if the droplet buildpack is included in the system buildpacks
func isBuildpackSupported(detectedBuildpack resource.DetectedBuildpack, systemBuildpacks map[string]resource.Buildpack) (bool, buildpackSource, *resource.Buildpack) {
	var source buildpackSource
	buildpackName := detectedBuildpack.Name

	isUsingPinnedBuildpack := isBuildpackFilenamePinnedToGitRepo(buildpackName)
	if isUsingPinnedBuildpack {
		buildpackName = getPinnedGitRepoBuildpackName(buildpackName)
		source = BUILDPACK_GIT_PINNED
	}

	if buildpackName == "" {
		return false, source, nil
	}

	buildpack, found := systemBuildpacks[buildpackName]
	if !found {
		return false, source, nil
	}

	return found, source, &buildpack
}

func isDropletBuildpackOutdated(
	dropletBuildpack *resource.DetectedBuildpack,
	dropletBuildpackSource buildpackSource,
	supportedBuildpackVersion string,
) bool {
	if dropletBuildpack == nil || supportedBuildpackVersion == "" {
		return false
	}

	var dropletBuildpackVersion string

	switch dropletBuildpackSource {
	case BUILDPACK_SYSTEM:
		dropletBuildpackVersion = fmt.Sprintf("v%s", dropletBuildpack.Version)
	case BUILDPACK_GIT_PINNED:
		dropletBuildpackVersion = getPinnedGitRepoBuildpackVersion(dropletBuildpack.Name)
	}

	if dropletBuildpackVersion == "" {
		return false
	}

	res := semver.Compare(dropletBuildpackVersion, supportedBuildpackVersion)
	return res == -1
}

func getBuildpackReleaseInfo(buildpack *resource.Buildpack) (*buildpackReleaseInfo, error) {
	if buildpack.Filename == nil {
		return nil, errors.New("no buildpack filename found")
	}

	version, err := parseSystemBuildpackVersion(*buildpack.Filename)
	if err != nil {
		return nil, err
	}

	buildpackReleaseURL := getBuildpackReleaseURL(buildpack.Name)
	if buildpack.Filename == nil {
		return nil, fmt.Errorf("Buildpack %s guid %s missing filename metadata; skipping release info\n", buildpack.Name, buildpack.GUID)
	}

	buildpackVersionURL := getBuildpackVersionURL(buildpackReleaseURL, version)

	return &buildpackReleaseInfo{
		BuildpackName:    buildpack.Name,
		BuildpackVersion: version,
		BuildpackURL:     buildpackVersionURL,
	}, nil
}
