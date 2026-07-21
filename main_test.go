package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cfclient "github.com/cloudfoundry/go-cfclient/v3/client"
	cfconfig "github.com/cloudfoundry/go-cfclient/v3/config"
	"github.com/cloudfoundry/go-cfclient/v3/resource"

	"github.com/cloud-gov/buildpack-notify/mocks"
	"github.com/stretchr/testify/mock"
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

func TestIsDropletUsingSupportedBuildpack(t *testing.T) {
	buildpacks := map[string]resource.Buildpack{
		"python_buildpack": {Name: "python_buildpack"},
	}
	testCases := []struct {
		name     string
		droplet  resource.Droplet
		expected bool
	}{
		{
			"supported buildpack",
			resource.Droplet{Buildpacks: []resource.DetectedBuildpack{{Name: "python_buildpack"}}},
			true,
		},
		{
			"unsupported buildpack",
			resource.Droplet{Buildpacks: []resource.DetectedBuildpack{{Name: "custom_buildpack"}}},
			false,
		},
		{
			"empty buildpack name is ignored",
			resource.Droplet{Buildpacks: []resource.DetectedBuildpack{{Name: ""}}},
			false,
		},
		{
			"no buildpacks",
			resource.Droplet{},
			false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, bp := isDropletUsingSupportedBuildpack(tc.droplet, buildpacks)
			if found != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, found)
			}
			if found && bp == nil {
				t.Errorf("Expected a buildpack pointer when found")
			}
			if !found && bp != nil {
				t.Errorf("Expected nil buildpack when not found")
			}
		})
	}
}

func TestIsDropletUsingOutdatedBuildpack(t *testing.T) {
	older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)

	testCases := []struct {
		name      string
		droplet   *resource.Droplet
		buildpack *resource.Buildpack
		expected  bool
	}{
		{
			"droplet older than buildpack -> outdated",
			&resource.Droplet{Resource: resource.Resource{CreatedAt: older}},
			&resource.Buildpack{Resource: resource.Resource{UpdatedAt: newer}},
			true,
		},
		{
			"droplet newer than buildpack -> not outdated",
			&resource.Droplet{Resource: resource.Resource{CreatedAt: newer}},
			&resource.Buildpack{Resource: resource.Resource{UpdatedAt: older}},
			false,
		},
		{
			"nil droplet -> not outdated",
			nil,
			&resource.Buildpack{Resource: resource.Resource{UpdatedAt: newer}},
			false,
		},
		{
			"nil buildpack -> not outdated",
			&resource.Droplet{Resource: resource.Resource{CreatedAt: older}},
			nil,
			false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDropletUsingOutdatedBuildpack(tc.droplet, tc.buildpack); got != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, got)
			}
		})
	}
}

const (
	user1     = "user1@example.com"
	user1GUID = "user1-guid"
	user2     = "user2@example.com"
	user2GUID = "user2-guid"
)

// appSpec describes an app fixture and the space it lives in.
type appSpec struct {
	guid      string
	name      string
	spaceGUID string
}

// spaceSpec describes a space fixture: its name, org name, and the space
// role assignments (username -> role type) returned by /v3/roles.
type spaceSpec struct {
	name    string
	orgName string
	roles   []roleSpec
}

type roleSpec struct {
	userGUID string
	username string
	roleType string // e.g. "space_manager", "space_developer", "space_auditor"
}

func v3App(spec appSpec) *resource.App {
	app := &resource.App{
		Name:  spec.name,
		State: "STARTED",
	}
	app.GUID = spec.guid
	app.Relationships.Space.Data = &resource.Relationship{GUID: spec.spaceGUID}
	return app
}

// newV3TestServerAndClient stands up a httptest server that emulates the
// subset of the v3 CF API used by findOwnersOfApps, and returns a connected
// client.
func newV3TestServerAndClient(t *testing.T, apps []appSpec, spaces map[string]spaceSpec) (*cfclient.Client, func()) {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoder := json.NewEncoder(w)
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/oauth/token":
			_ = encoder.Encode(map[string]any{
				"access_token": "fake-token",
				"token_type":   "bearer",
				"expires_in":   3600,
			})

		case r.URL.Path == "/v3/apps":
			resources := make([]*resource.App, 0, len(apps))
			for _, a := range apps {
				resources = append(resources, v3App(a))
			}
			_ = encoder.Encode(resource.AppList{Resources: resources})

		case strings.HasPrefix(r.URL.Path, "/v3/spaces/"):
			spaceGUID := strings.TrimPrefix(r.URL.Path, "/v3/spaces/")
			spec := spaces[spaceGUID]

			space := &resource.Space{Name: spec.name}
			space.GUID = spaceGUID
			org := &resource.Organization{Name: spec.orgName}
			org.GUID = spaceGUID + "-org"

			_ = encoder.Encode(resource.SpaceWithIncluded{
				Space:    *space,
				Included: &resource.SpaceIncluded{Organizations: []*resource.Organization{org}},
			})

		case r.URL.Path == "/v3/roles":
			spaceGUIDs := r.URL.Query()["space_guids"]
			var wanted string
			if len(spaceGUIDs) > 0 {
				wanted = spaceGUIDs[0]
			}
			spec := spaces[wanted]

			roleTypeFilter := map[string]bool{}
			for _, t := range r.URL.Query()["types"] {
				for _, part := range strings.Split(t, ",") {
					roleTypeFilter[part] = true
				}
			}

			var roles []*resource.Role
			usersByGUID := map[string]*resource.User{}
			for _, role := range spec.roles {
				if len(roleTypeFilter) > 0 && !roleTypeFilter[role.roleType] {
					continue
				}
				rl := &resource.Role{Type: role.roleType}
				rl.Relationships.User.Data = &resource.Relationship{GUID: role.userGUID}
				roles = append(roles, rl)

				if _, ok := usersByGUID[role.userGUID]; !ok {
					username := role.username
					u := &resource.User{Username: &username}
					u.GUID = role.userGUID
					usersByGUID[role.userGUID] = u
				}
			}

			users := make([]*resource.User, 0, len(usersByGUID))
			for _, u := range usersByGUID {
				users = append(users, u)
			}

			_ = encoder.Encode(resource.RoleList{
				Resources: roles,
				Included:  &resource.RoleIncluded{Users: users},
			})

		default:
			t.Errorf("Unhandled path in test server: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	ts := httptest.NewServer(handler)

	cfg, err := cfconfig.New(
		ts.URL,
		cfconfig.ClientCredentials("client-id", "client-secret"),
		// Skip API root discovery by supplying the auth endpoints directly.
		cfconfig.AuthTokenURL(ts.URL, ts.URL),
	)
	if err != nil {
		ts.Close()
		t.Fatalf("Unable to create test config: %s", err)
	}
	client, err := cfclient.New(cfg)
	if err != nil {
		ts.Close()
		t.Fatalf("Unable to create test client: %s", err)
	}

	return client, ts.Close
}

func TestFindOwnersOfApps(t *testing.T) {
	testCases := []struct {
		name     string
		apps     []appSpec
		spaces   map[string]spaceSpec
		expected map[string][]string // username -> app names
	}{
		{
			"single app, single user",
			[]appSpec{{guid: "app1", name: "app1", spaceGUID: "space1"}},
			map[string]spaceSpec{
				"space1": {name: "dev", orgName: "sandbox", roles: []roleSpec{
					{user1GUID, user1, "space_manager"},
				}},
			},
			map[string][]string{user1: {"app1"}},
		},
		{
			"single app, single user multiple valid roles",
			[]appSpec{{guid: "app1", name: "app1", spaceGUID: "space1"}},
			map[string]spaceSpec{
				"space1": {name: "dev", orgName: "sandbox", roles: []roleSpec{
					{user1GUID, user1, "space_manager"},
					{user1GUID, user1, "space_developer"},
				}},
			},
			// The v3 /v3/roles?include=user response returns each user once in
			// `included.users`, so a user with two owner roles is notified once.
			map[string][]string{user1: {"app1"}},
		},
		{
			"single app, single user no valid role (auditor filtered by API)",
			[]appSpec{{guid: "app1", name: "app1", spaceGUID: "space1"}},
			map[string]spaceSpec{
				"space1": {name: "dev", orgName: "sandbox", roles: []roleSpec{
					{user1GUID, user1, "space_auditor"},
				}},
			},
			map[string][]string{},
		},
		{
			"same single app, multiple users",
			[]appSpec{{guid: "app1", name: "app1", spaceGUID: "space1"}},
			map[string]spaceSpec{
				"space1": {name: "dev", orgName: "sandbox", roles: []roleSpec{
					{user1GUID, user1, "space_manager"},
					{user2GUID, user2, "space_manager"},
				}},
			},
			map[string][]string{
				user1: {"app1"},
				user2: {"app1"},
			},
		},
		{
			"same single app, multiple users, one without valid role",
			[]appSpec{{guid: "app1", name: "app1", spaceGUID: "space1"}},
			map[string]spaceSpec{
				"space1": {name: "dev", orgName: "sandbox", roles: []roleSpec{
					{user1GUID, user1, "space_auditor"},
					{user2GUID, user2, "space_manager"},
				}},
			},
			map[string][]string{
				user2: {"app1"},
			},
		},
		{
			"two apps in different spaces, two users, mutually exclusive ownership",
			[]appSpec{
				{guid: "app1", name: "app1", spaceGUID: "space1"},
				{guid: "app2", name: "app2", spaceGUID: "space2"},
			},
			map[string]spaceSpec{
				"space1": {name: "dev", orgName: "sandbox", roles: []roleSpec{
					{user1GUID, user1, "space_manager"},
				}},
				"space2": {name: "staging", orgName: "paid-org", roles: []roleSpec{
					{user2GUID, user2, "space_manager"},
				}},
			},
			map[string][]string{
				user1: {"app1"},
				user2: {"app2"},
			},
		},
		{
			"two apps in different spaces, two users with ownership in both spaces",
			[]appSpec{
				{guid: "app1", name: "app1", spaceGUID: "space1"},
				{guid: "app2", name: "app2", spaceGUID: "space2"},
			},
			map[string]spaceSpec{
				"space1": {name: "dev", orgName: "sandbox", roles: []roleSpec{
					{user1GUID, user1, "space_manager"},
					{user2GUID, user2, "space_manager"},
				}},
				"space2": {name: "staging", orgName: "paid-org", roles: []roleSpec{
					{user1GUID, user1, "space_manager"},
					{user2GUID, user2, "space_manager"},
				}},
			},
			map[string][]string{
				user1: {"app1", "app2"},
				user2: {"app1", "app2"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, closeFn := newV3TestServerAndClient(t, tc.apps, tc.spaces)
			defer closeFn()

			ctx := context.Background()
			apps, err := client.Applications.ListAll(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}

			actual := findOwnersOfApps(ctx, apps, client)
			if len(actual) != len(tc.expected) {
				t.Errorf("Test %s failed. Expected %d user entries, found %d\n", tc.name, len(tc.expected), len(actual))
			}
			for actualUsername, actualApps := range actual {
				expectedAppNames, found := tc.expected[actualUsername]
				if !found {
					t.Errorf("Test %s failed. Unexpected user %s in result\n", tc.name, actualUsername)
					continue
				}
				if len(expectedAppNames) != len(actualApps) {
					t.Errorf("Test %s failed. For user %s expected %d apps, found %d\n", tc.name, actualUsername, len(expectedAppNames), len(actualApps))
					continue
				}
				for _, actualApp := range actualApps {
					matched := false
					for _, expectedName := range expectedAppNames {
						if expectedName == actualApp.Name {
							matched = true
							break
						}
					}
					if !matched {
						t.Errorf("Test %s failed. App %s not expected for user %s\n", tc.name, actualApp.Name, actualUsername)
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
		usersAndApps  map[string][]notifyApp
		expectedCalls []testNotifyEmail
	}{
		{
			"single user, single app",
			map[string][]notifyApp{
				"james@example.com": {
					{Name: "testapp"},
				},
			},
			[]testNotifyEmail{
				{
					notifyEmail{
						"james@example.com",
						[]notifyApp{
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
			map[string][]notifyApp{
				"james@example.com": {
					{Name: "testapp1"},
					{Name: "testapp2"},
				},
			},
			[]testNotifyEmail{
				{
					notifyEmail{
						"james@example.com",
						[]notifyApp{
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
			map[string][]notifyApp{
				"james@example.com": {
					{Name: "testapp1"},
				},
				"bob@example.com": {
					{Name: "testapp2"},
				},
			},
			[]testNotifyEmail{
				{
					notifyEmail{
						"james@example.com",
						[]notifyApp{
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
						[]notifyApp{
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
			map[string][]notifyApp{
				"james@example.com": {
					{Name: "testapp1"},
					{Name: "testapp2"},
				},
				"bob@example.com": {
					{Name: "testapp3"},
					{Name: "testapp4"},
				},
			},
			[]testNotifyEmail{
				{
					notifyEmail{
						"james@example.com",
						[]notifyApp{
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
						[]notifyApp{
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
