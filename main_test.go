package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/18F/cg-buildpack-notify/mocks"
	"github.com/cloudfoundry-community/go-cfclient"
	"github.com/stretchr/testify/mock"
)

func TestSpaceUserHasRoles(t *testing.T) {
	testCases := []struct {
		name         string
		rolesToCheck map[string]bool
		spaceUser    cfclient.SpaceRole
		expected     bool
	}{
		{"role there", map[string]bool{"test": true}, cfclient.SpaceRole{SpaceRoles: []string{"test"}}, true},
		{"role not there", map[string]bool{"test": true}, cfclient.SpaceRole{SpaceRoles: []string{""}}, false},
		{"multiple roles not there", map[string]bool{"test1": true, "test2": true}, cfclient.SpaceRole{SpaceRoles: []string{"foo"}}, false},
		{"multiple roles there", map[string]bool{"test1": true, "test2": true}, cfclient.SpaceRole{SpaceRoles: []string{"test2", "test"}}, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if ret := spaceUserHasRoles(tc.spaceUser, tc.rolesToCheck); ret != tc.expected {
				t.Errorf("Test %s failed. Expected %v Actual %v\n", tc.name, tc.expected, ret)
			}
		})
	}
}

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

	buildpackVersion := parseBuildpackVersion(testBuildpackFileName)

	if buildpackVersion != expectedBuildpackVersion {
		t.Errorf("The buildpack version for %s was not parsed correctly; expected %s", testBuildpackFileName, expectedBuildpackVersion)
	}
}

func TestParseBuildpackVersionMoreDashes(t *testing.T) {
	testBuildpackFileName := "php-buildpack-cflinuxfs3-v4.4.49.zip"
	expectedBuildpackVersion := "v4.4.49"

	buildpackVersion := parseBuildpackVersion(testBuildpackFileName)

	if buildpackVersion != expectedBuildpackVersion {
		t.Errorf("The buildpack version for %s was not parsed correctly; expected %s", testBuildpackFileName, expectedBuildpackVersion)
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

type spaceSpec struct {
	space      cfclient.SpaceResource
	spaceRoles cfclient.SpaceRoleResponse
}

const (
	user1     = "user1@example.com"
	user1GUID = "user1-guid"
	user2     = "user2@example.com"
	user2GUID = "user2-guid"
)

func TestFindOwnersOfApps(t *testing.T) {
	testCases := []struct {
		name     string
		apps     cfclient.AppResponse
		spaces   map[string]spaceSpec
		expected map[string][]cfclient.App
	}{
		{
			"single app, single user",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_manager"}}}}},
				},
			},
			map[string][]cfclient.App{user1: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}},
		},
		{
			"single app, single user multiple valid roles",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_manager", "space_developer"}}}}},
				},
			},
			map[string][]cfclient.App{user1: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}},
		},
		{
			"single app, single user one valid role, one invalid role",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_manager", "space_auditor"}}}}},
				},
			},
			map[string][]cfclient.App{user1: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}},
		},
		{
			"single app, single user no valid role",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_auditor"}}}}},
				},
			},
			map[string][]cfclient.App{},
		},
		{
			"same single app, multiple users",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_manager"}}},
						{Meta: cfclient.Meta{Guid: user2GUID}, Entity: cfclient.SpaceRole{Username: user2, SpaceRoles: []string{"space_manager"}}},
					}},
				},
			},
			map[string][]cfclient.App{
				user1: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}},
				user2: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}},
			},
		},
		{
			"same single app, multiple users, one without valid role",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_auditor"}}},
						{Meta: cfclient.Meta{Guid: user2GUID}, Entity: cfclient.SpaceRole{Username: user2, SpaceRoles: []string{"space_manager"}}},
					}},
				},
			},
			map[string][]cfclient.App{
				user2: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}},
			},
		},
		{
			"two apps in different spaces, two users, mutually exclusive app ownership",
			cfclient.AppResponse{Resources: []cfclient.AppResource{
				{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}},
				{Meta: cfclient.Meta{Guid: "app2"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space2", SpaceGuid: "space2"}},
			}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_manager"}}},
					}},
				},
				"space2": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space2"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user2, SpaceRoles: []string{"space_manager"}}},
					}},
				},
			},
			map[string][]cfclient.App{
				user1: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}},
				user2: []cfclient.App{cfclient.App{Guid: "app2", SpaceURL: "/v2/spaces/space2", SpaceGuid: "space2"}},
			},
		},
		{
			"two apps in different spaces, two users with ownership in both spaces",
			cfclient.AppResponse{Resources: []cfclient.AppResource{
				{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}},
				{Meta: cfclient.Meta{Guid: "app2"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space2", SpaceGuid: "space2"}},
			}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_manager"}}},
						{Meta: cfclient.Meta{Guid: user2GUID}, Entity: cfclient.SpaceRole{Username: user2, SpaceRoles: []string{"space_manager"}}},
					}},
				},
				"space2": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space2"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Meta: cfclient.Meta{Guid: user1GUID}, Entity: cfclient.SpaceRole{Username: user1, SpaceRoles: []string{"space_manager"}}},
						{Meta: cfclient.Meta{Guid: user2GUID}, Entity: cfclient.SpaceRole{Username: user2, SpaceRoles: []string{"space_manager"}}},
					}},
				},
			},
			map[string][]cfclient.App{
				user1: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}, cfclient.App{Guid: "app2", SpaceURL: "/v2/spaces/space2", SpaceGuid: "space2"}},
				user2: []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1", SpaceGuid: "space1"}, cfclient.App{Guid: "app2", SpaceURL: "/v2/spaces/space2", SpaceGuid: "space2"}},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				encoder := json.NewEncoder(w)
				parts := strings.Split(r.URL.Path, "/")
				if r.URL.Path == "/v2/apps" {
					encoder.Encode(tc.apps)
				} else if strings.HasSuffix(r.URL.Path, "user_roles") {
					encoder.Encode(tc.spaces[parts[len(parts)-2]].spaceRoles)
				} else if len(parts) >= 3 {
					encoder.Encode(tc.spaces[parts[3]].space)
				} else {
					t.Fatalf("Unable to find handler for path %s", r.URL.Path)
				}
			}))
			defer ts.Close()
			c := cfclient.Client{Config: cfclient.Config{HttpClient: http.DefaultClient, ApiAddress: ts.URL}}
			apps, err := c.ListApps()
			if err != nil {
				t.Fatal(err)
			}
			actual := findOwnersOfApps(apps, &c)
			if len(actual) != len(tc.expected) {
				t.Errorf("Test %s failed. Expected %d user entries, only found %d\n", tc.name, len(tc.expected), len(actual))
			}
			for actualUsername, actualOutdatedApps := range actual {
				expectedOutdatedApps, found := tc.expected[actualUsername]
				if !found {
					t.Errorf("Test %s failed. Couldn't find user %s in expected map\n", tc.name, actualUsername)
					continue
				}
				if len(expectedOutdatedApps) != len(actualOutdatedApps) {
					t.Errorf("Test %s failed. Expected %d outdated apps, only found %d\n", tc.name, len(expectedOutdatedApps), len(actualOutdatedApps))
					t.Errorf("Expected %+v\nActual %+v\n", expectedOutdatedApps, actualOutdatedApps)
					continue
				}
				for _, actualOutdatedApp := range actualOutdatedApps {
					found := false
					for _, expectedOutdatedApp := range expectedOutdatedApps {
						if expectedOutdatedApp.Guid == actualOutdatedApp.Guid {
							found = true
						}
					}
					if !found {
						t.Errorf("Test %s failed. Looked for app with guid %s, Could not find it", tc.name, actualOutdatedApp.Guid)
					}
				}
			}
		})
	}
}

type testNotifyEmail struct {
	notifyEmail
	subject string
}

func TestSendNotifyEmailToUsers(t *testing.T) {
	updatedBuildpacks := []buildpackReleaseInfo{
		{
			"java_buildpack",
			"v4.41",
			"https://github.com/cloudfoundry/java-buildpack/releases/tags/v4.41",
		},
		{
			"python_buildpack",
			"v1.7.43",
			"https://github.com/cloudfoundry/python-buildpack/releases/tags/v1.7.43",
		},
		{
			"ruby_buildpack",
			"v1.8.43",
			"https://github.com/cloudfoundry/ruby-buildpack/releases/tags/v1.8.43",
		},
	}

	testCases := []struct {
		name          string
		usersAndApps  map[string][]cfclient.App
		expectedCalls []testNotifyEmail
	}{
		{
			"single user, single app",
			map[string][]cfclient.App{
				"james@example.com": []cfclient.App{
					{Name: "testapp"},
				},
			},
			[]testNotifyEmail{
				{
					notifyEmail{
						"james@example.com",
						[]cfclient.App{
							{Name: "testapp"},
						},
						false,
						updatedBuildpacks,
					},
					"Action required: restage your application",
				},
			},
		},
		{
			"single user, multiple apps",
			map[string][]cfclient.App{
				"james@example.com": []cfclient.App{
					{Name: "testapp1"},
					{Name: "testapp2"},
				},
			},
			[]testNotifyEmail{
				{
					notifyEmail{
						"james@example.com",
						[]cfclient.App{
							{Name: "testapp1"},
							{Name: "testapp2"},
						},
						true,
						updatedBuildpacks,
					},
					"Action required: restage your applications",
				},
			},
		},
		{
			"multiple users, each with a single app",
			map[string][]cfclient.App{
				"james@example.com": []cfclient.App{
					{Name: "testapp1"},
				},
				"bob@example.com": []cfclient.App{
					{Name: "testapp2"},
				},
			},
			[]testNotifyEmail{
				{
					notifyEmail{
						"james@example.com",
						[]cfclient.App{
							{Name: "testapp1"},
						},
						false,
						updatedBuildpacks,
					},
					"Action required: restage your application",
				},
				{
					notifyEmail{
						"bob@example.com",
						[]cfclient.App{
							{Name: "testapp2"},
						},
						false,
						updatedBuildpacks,
					},
					"Action required: restage your application",
				},
			},
		},
		{
			"multiple users, each with multiple apps",
			map[string][]cfclient.App{
				"james@example.com": []cfclient.App{
					{Name: "testapp1"},
					{Name: "testapp2"},
				},
				"bob@example.com": []cfclient.App{
					{Name: "testapp3"},
					{Name: "testapp4"},
				},
			},
			[]testNotifyEmail{
				{
					notifyEmail{
						"james@example.com",
						[]cfclient.App{
							{Name: "testapp1"},
							{Name: "testapp2"},
						},
						true,
						updatedBuildpacks,
					},
					"Action required: restage your applications",
				},
				{
					notifyEmail{
						"bob@example.com",
						[]cfclient.App{
							{Name: "testapp3"},
							{Name: "testapp4"},
						},
						true,
						updatedBuildpacks,
					},
					"Action required: restage your applications",
				},
			},
		},
	}

	for _, tc := range testCases {
		templates, _ := initTemplates()
		t.Run(tc.name, func(t *testing.T) {
			mockMailer := new(mocks.Mailer)
			mockMailer.On("SendEmail", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			sendNotifyEmailToUsers(tc.usersAndApps, updatedBuildpacks, templates, mockMailer, false)
			if !mockMailer.AssertNumberOfCalls(t, "SendEmail", len(tc.expectedCalls)) {
				t.Errorf("Did not call send e-mail the number of expected times")
				t.Log(len(mockMailer.Calls))
			}
			count := 0
			for _, expectedCall := range tc.expectedCalls {
				for _, call := range mockMailer.Calls {
					if call.Method == "SendEmail" && call.Arguments.String(0) == expectedCall.Username {
						if call.Arguments.String(1) != expectedCall.subject {
							t.Errorf("Failed to match subject line. Found %s, Expected %s", call.Arguments.String(1), expectedCall.subject)
							continue
						}
						raw := call.Arguments.Get(2).([]byte)
						rawString := string(raw)
						foundApps := true
						for _, app := range expectedCall.Apps {
							if !strings.Contains(rawString, app.Name) {
								t.Errorf("Unable to find app name in e-mail %s", app.Name)
								foundApps = false
							}
						}
						if foundApps {
							count++
						}
					}
				}
			}
			// Sanity check.
			if count != len(tc.expectedCalls) {
				t.Error("Something unexpected happened which caused a mismatch number of calls")
			}
		})
	}
}
