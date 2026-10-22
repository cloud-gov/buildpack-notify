package main

import (
	"testing"

	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"github.com/stretchr/testify/assert"
)

func TestBuildPackURLIsReturnedForSystemBuildPacks(t *testing.T) {
	testBuildPackNames := []string{
		"staticfile_buildpack",
		"java_buildpack",
		"ruby_buildpack",
		"dotnet_core_buildpack",
		"nodejs_buildpack",
		"go_buildpack",
		"python_buildpack",
		"php_buildpack",
		"binary_buildpack",
		"nginx_buildpack",
		"r_buildpack",
	}

	for _, testBuildPackName := range testBuildPackNames {
		testBuildPackURL := getBuildpackReleaseURL(testBuildPackName)

		if testBuildPackURL == "" {
			t.Errorf("Finding the buildpack URL failed for %s.", testBuildPackName)
		}
	}
}

func TestEmptyStringReturnedForUnknownBuildpack(t *testing.T) {
	testBuildpackName := "my_fake_buildpack"

	testBuildpackURL := getBuildpackReleaseURL(testBuildpackName)

	if testBuildpackURL != "" {
		t.Errorf("The buildpack %s should not have mapped to a URL.", testBuildpackName)
	}
}

func TestParseBuildpackVersion(t *testing.T) {
	testBuildpackFileName := "python_buildpack-cflinuxfs3-v1.7.43.zip"
	expectedBuildpackVersion := "v1.7.43"

	buildpackVersion, err := parseBuildpackVersion(testBuildpackFileName)
	if err != nil {
		t.Fatal(err)
	}
	if buildpackVersion != expectedBuildpackVersion {
		t.Errorf("The buildpack version for %s was not parsed correctly; expected %s", testBuildpackFileName, expectedBuildpackVersion)
	}
}

func TestParseBuildpackVersionMoreDashes(t *testing.T) {
	testBuildpackFileName := "php-buildpack-cflinuxfs3-v4.4.49.zip"
	expectedBuildpackVersion := "v4.4.49"

	buildpackVersion, err := parseBuildpackVersion(testBuildpackFileName)
	if err != nil {
		t.Fatal(err)
	}
	if buildpackVersion != expectedBuildpackVersion {
		t.Errorf("The buildpack version for %s was not parsed correctly; expected %s", testBuildpackFileName, expectedBuildpackVersion)
	}
}

func TestParseBuildpackVersionGitRepo(t *testing.T) {
	testBuildpackFileName := "https://github.com/cloudfoundry/python-buildpack.git#v1.8.38"
	expectedBuildpackVersion := "v1.8.38"

	buildpackVersion, err := parseBuildpackVersion(testBuildpackFileName)
	if err != nil {
		t.Fatal(err)
	}
	if buildpackVersion != expectedBuildpackVersion {
		t.Errorf("The buildpack version for %s was not parsed correctly; expected %s, got %s", testBuildpackFileName, expectedBuildpackVersion, buildpackVersion)
	}
}

func TestBuildpackVersionURL(t *testing.T) {
	testBuildpackReleaseURL := "https://github.com/cloudfoundry/python-buildpack/releases"
	testBuildpackVersion := "v1.7.43"
	expectedVersionURL := "https://github.com/cloudfoundry/python-buildpack/releases/tag/v1.7.43"

	buildpackVersionURL := getBuildpackVersionURL(testBuildpackReleaseURL, testBuildpackVersion)

	if buildpackVersionURL != expectedVersionURL {
		t.Errorf("The buildpack version URL for %s (%s) was not built correctly; expected %s", testBuildpackReleaseURL, testBuildpackVersion, expectedVersionURL)
	}
}

func TestBuildpackVersionURLWithBadVersion(t *testing.T) {
	testBuildpackReleaseURL := "https://github.com/cloudfoundry/python-buildpack/releases"
	testBuildpackVersionMissingV := "7.5"
	testBuildpackVersionDifferentFormat := "x.321.y.323"
	expectedVersionURL := "https://github.com/cloudfoundry/python-buildpack/releases"

	buildpackVersionURL1 := getBuildpackVersionURL(testBuildpackReleaseURL, testBuildpackVersionMissingV)
	buildpackVersionURL2 := getBuildpackVersionURL(testBuildpackReleaseURL, testBuildpackVersionDifferentFormat)

	if buildpackVersionURL1 != expectedVersionURL {
		t.Errorf("The buildpack version URL for %s (%s) was not built correctly; expected %s", testBuildpackReleaseURL, testBuildpackVersionMissingV, expectedVersionURL)
	}

	if buildpackVersionURL2 != expectedVersionURL {
		t.Errorf("The buildpack version URL for %s (%s) was not built correctly; expected %s", testBuildpackReleaseURL, testBuildpackVersionDifferentFormat, expectedVersionURL)
	}
}

func TestIsBuildpackSupported(t *testing.T) {
	buildpacks := map[string]resource.Buildpack{
		"python_buildpack": {Name: "python_buildpack"},
	}
	testCases := []struct {
		name             string
		dropletBuildpack resource.DetectedBuildpack
		expectSupported  bool
		expected         *buildpackInfo
	}{
		{
			"supported buildpack",
			resource.DetectedBuildpack{Name: "python_buildpack"},
			true,
			&buildpackInfo{
				source: BUILDPACK_SYSTEM,
				buildpack: &resource.Buildpack{
					Name: "python_buildpack",
				},
			},
		},
		{
			"unsupported buildpack",
			resource.DetectedBuildpack{Name: "custom_buildpack"},
			false,
			&buildpackInfo{},
		},
		{
			"empty buildpack name is ignored",
			resource.DetectedBuildpack{Name: ""},
			false,
			&buildpackInfo{},
		},
		{
			"no buildpacks",
			resource.DetectedBuildpack{},
			false,
			&buildpackInfo{},
		},
		{
			"pinned git repo buildpack",
			resource.DetectedBuildpack{Name: "https://github.com/cloudfoundry/python-buildpack.git#v1.8.38"},
			true,
			&buildpackInfo{
				source: BUILDPACK_GIT_PINNED,
				buildpack: &resource.Buildpack{
					Name: "python_buildpack",
				},
			},
		},
		{
			"invalid git repo pinned version",
			resource.DetectedBuildpack{Name: "https://github.com/cloudfoundry/python-buildpack.git#v1.8"},
			false,
			&buildpackInfo{},
		},
		{
			"pinned git repo sha",
			resource.DetectedBuildpack{Name: "https://github.com/cloudfoundry/python-buildpack.git#abc1234"},
			false,
			&buildpackInfo{},
		},
		{
			"invalid git repo",
			resource.DetectedBuildpack{Name: "https://gitlab.com/cloudfoundry/python-buildpack.git#v1.8.38"},
			false,
			&buildpackInfo{},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isSupported, supportedBuildpack := isBuildpackSupported(tc.dropletBuildpack, buildpacks)
			assert.Equal(t, isSupported, tc.expectSupported)
			assert.Equal(t, supportedBuildpack, tc.expected)
		})
	}
}

func TestIsDropletUsingOutdatedBuildpack(t *testing.T) {
	testCases := []struct {
		name                      string
		dropletBuildpack          *resource.DetectedBuildpack
		source                    buildpackSource
		supportedBuildpackVersion string
		expected                  bool
	}{
		{
			"droplet older than buildpack -> outdated",
			&resource.DetectedBuildpack{Name: "python_buildpack", Version: "1.2.3"},
			BUILDPACK_SYSTEM,
			"v1.2.4",
			true,
		},
		{
			"droplet newer than buildpack -> not outdated",
			&resource.DetectedBuildpack{Name: "python_buildpack", Version: "1.2.3"},
			BUILDPACK_SYSTEM,
			"v1.2.3",
			false,
		},
		{
			"nil droplet -> not outdated",
			nil,
			BUILDPACK_SYSTEM,
			"v1.2.3",
			false,
		},
		{
			"empty buildpack version -> not outdated",
			&resource.DetectedBuildpack{Name: "python_buildpack", Version: "1.2.3"},
			BUILDPACK_SYSTEM,
			"",
			false,
		},
		{
			"pinned git buildpack is outdated",
			&resource.DetectedBuildpack{Name: "https://github.com/cloudfoundry/python-buildpack.git#v1.2.3"},
			BUILDPACK_GIT_PINNED,
			"v1.2.4",
			true,
		},
		{
			"pinned git buildpack is not outdated",
			&resource.DetectedBuildpack{Name: "https://github.com/cloudfoundry/python-buildpack.git#v1.2.5"},
			BUILDPACK_GIT_PINNED,
			"v1.2.4",
			false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDropletBuildpackOutdated(tc.dropletBuildpack, tc.source, tc.supportedBuildpackVersion); got != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestIsBuildpackFilenamePinnedToGitRepo(t *testing.T) {
	assert.True(t, isBuildpackFilenamePinnedToGitRepo("https://github.com/cloudfoundry/python-buildpack.git#v1.8.38"))
	assert.False(t, isBuildpackFilenamePinnedToGitRepo("foobar"))
	assert.False(t, isBuildpackFilenamePinnedToGitRepo("php-buildpack-cflinuxfs3-v4.4.49.zip"))
}

func TestGetPinnedGitRepoBuildpackName(t *testing.T) {
	assert.Equal(t, getPinnedGitRepoBuildpackName("https://github.com/cloudfoundry/python-buildpack.git#v1.8.38"), "python_buildpack")
	assert.Equal(t, getPinnedGitRepoBuildpackName("https://github.com/cloudfoundry/foobar-buildpack.git#v1.2.3"), "foobar_buildpack")
	assert.Equal(t, getPinnedGitRepoBuildpackName("php-buildpack-cflinuxfs3-v4.4.49.zip"), "")
}
