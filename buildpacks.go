package main

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"golang.org/x/mod/semver"
)

var (
	buildpackZipFileRegex = regexp.MustCompile(`^[a-z]+(_|-)buildpack(_|-)cflinuxfs[0-9]{1}(_|-)(v[0-9]+.[0-9]+.[0-9]+).zip$`)
	buildpackGitRepoRegex = regexp.MustCompile(`^https://github.com/cloudfoundry/[a-z]+-buildpack.git#(v[0-9]+.[0-9]+.[0-9]+)$`)
)

type buildpackReleaseInfo struct {
	BuildpackName    string
	BuildpackVersion string
	BuildpackURL     string
}

func getSupportedBuildpackFilenameRegexes() []*regexp.Regexp {
	return []*regexp.Regexp{buildpackZipFileRegex, buildpackGitRepoRegex}
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

func parseBuildpackVersion(buildpackFileName string) (string, error) {
	// Takes a buildpack file name and parses out the version number from it.
	// Buildpack filenames currently look like this: python_buildpack-cflinuxfs3-v1.7.43.zip
	// "v1.7.43" is the version in this case.

	for _, regex := range getSupportedBuildpackFilenameRegexes() {
		if regex.Match([]byte(buildpackFileName)) {
			match := regex.FindStringSubmatch(buildpackFileName)
			if len(match) == 0 {
				return "", errors.New("could not parse version")
			}
			version := match[len(match)-1]
			if !semver.IsValid(version) {
				return "", fmt.Errorf("%s is not a valid semantic version", version)
			}
			return version, nil
		}
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
	versionRe := regexp.MustCompile(`^v[0-9]+\.[0-9]+(\.[0-9]+)?$`)
	versionMatch := versionRe.FindAllString(buildpackVersion, -1)

	if versionMatch != nil {
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

// isDropletUsingSupportedBuildpack checks the buildpacks the droplet is using and comparing to see if one of them
// is a provided system buildpack.
func isDropletUsingSupportedBuildpack(droplet resource.Droplet, systemBuildpacks map[string]resource.Buildpack) (bool, *resource.Buildpack) {
	for _, dropletBuildpack := range droplet.Buildpacks {
		if buildpack, found := systemBuildpacks[dropletBuildpack.Name]; found && dropletBuildpack.Name != "" {
			return true, &buildpack
		}
	}
	return false, nil
}

func isDropletUsingOutdatedBuildpack(
	droplet *resource.Droplet,
	buildpack *resource.Buildpack,
) bool {
	if droplet == nil || buildpack == nil {
		return false
	}

	// for _, dropletBp := range(droplet.Buildpacks) {
	// 	if dropletBp.Version
	// }

	return droplet.CreatedAt.Before(buildpack.UpdatedAt)
}
