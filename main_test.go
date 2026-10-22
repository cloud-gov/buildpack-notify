package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	cfclient "github.com/cloudfoundry/go-cfclient/v3/client"
	cfconfig "github.com/cloudfoundry/go-cfclient/v3/config"
	"github.com/cloudfoundry/go-cfclient/v3/resource"

	"github.com/cloud-gov/buildpack-notify/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestFilterForValidEmailUsernames(t *testing.T) {
	valid := "valid@example.com"
	invalid := "not-an-email"
	app := notifyApp{
		Name:      "test-app",
		SpaceGUID: "space-guid",
	}

	filtered := filterForValidEmailUsernames([]*resource.User{
		{Username: &valid},
		{Username: &invalid},
		{Resource: resource.Resource{GUID: "missing-username-guid"}},
	}, app)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 valid user, got %d", len(filtered))
	}
	if filtered[0].Username == nil || *filtered[0].Username != valid {
		t.Fatalf("expected valid user to be retained")
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

type roleRequestExpectation struct {
	roleTypes        []string
	requireInclusion bool
}

type dropletSpec struct {
	forAppGUID string
	createdAt  time.Time
	buildpacks []resource.DetectedBuildpack
	statusCode int
}

type buildpackSpec struct {
	guid      string
	name      string
	updatedAt time.Time
	filename  *string
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
func newV3TestServerAndClient(t *testing.T, apps []appSpec, spaces map[string]spaceSpec, roleExpectation *roleRequestExpectation, buildpacks []buildpackSpec, droplets map[string]dropletSpec) (*cfclient.Client, func()) {
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

		case r.URL.Path == "/v3/buildpacks":
			resources := make([]*resource.Buildpack, 0, len(buildpacks))
			for _, bp := range buildpacks {
				resources = append(resources, &resource.Buildpack{
					Resource: resource.Resource{GUID: bp.guid, UpdatedAt: bp.updatedAt},
					Name:     bp.name,
					Filename: bp.filename,
				})
			}
			_ = encoder.Encode(resource.BuildpackList{Resources: resources})

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
			if roleExpectation != nil {
				assertRoleRequest(t, r.URL.Query(), wanted, *roleExpectation)
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

		case strings.HasPrefix(r.URL.Path, "/v3/apps/") && strings.HasSuffix(r.URL.Path, "/droplets/current"):
			appGUID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v3/apps/"), "/droplets/current")
			droplet, ok := droplets[appGUID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = encoder.Encode(map[string]any{"errors": []map[string]string{{"detail": "droplet not found"}}})
				return
			}
			if droplet.statusCode != 0 && droplet.statusCode != http.StatusOK {
				w.WriteHeader(droplet.statusCode)
				_ = encoder.Encode(map[string]any{"errors": []map[string]string{{"detail": "droplet error"}}})
				return
			}
			_ = encoder.Encode(resource.Droplet{
				Resource:   resource.Resource{CreatedAt: droplet.createdAt},
				Buildpacks: droplet.buildpacks,
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

func assertRoleRequest(t *testing.T, values url.Values, wanted string, expected roleRequestExpectation) {
	t.Helper()

	spaceGUIDs := values["space_guids"]
	if len(spaceGUIDs) != 1 || spaceGUIDs[0] != wanted {
		t.Errorf("expected space_guids=%s, got %v", wanted, spaceGUIDs)
	}

	actualTypes := splitAndSort(values["types"])
	expectedTypes := append([]string(nil), expected.roleTypes...)
	sort.Strings(expectedTypes)
	if len(actualTypes) != len(expectedTypes) {
		t.Errorf("expected role types %v, got %v", expectedTypes, actualTypes)
	} else {
		for i := range expectedTypes {
			if actualTypes[i] != expectedTypes[i] {
				t.Errorf("expected role types %v, got %v", expectedTypes, actualTypes)
				break
			}
		}
	}

	includeUsers := false
	for _, includeValue := range values["include"] {
		for _, part := range strings.Split(includeValue, ",") {
			if part == "user" {
				includeUsers = true
			}
		}
	}
	if expected.requireInclusion && !includeUsers {
		t.Errorf("expected include=user, got %v", values["include"])
	}
}

func splitAndSort(values []string) []string {
	var parts []string
	for _, value := range values {
		parts = append(parts, strings.Split(value, ",")...)
	}
	sort.Strings(parts)
	return parts
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
			client, closeFn := newV3TestServerAndClient(t, tc.apps, tc.spaces, &roleRequestExpectation{
				roleTypes:        []string{"space_developer", "space_manager"},
				requireInclusion: true,
			}, nil, nil)
			defer closeFn()

			ctx := context.Background()
			apps, err := client.Applications.ListAll(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}

			var notifyApps []notifyApp
			for _, app := range apps {
				notifyApps = append(notifyApps, notifyApp{
					Name:      app.Name,
					GUID:      app.GUID,
					SpaceGUID: app.Relationships.Space.Data.GUID,
				})
			}

			actual := findOwnersOfApps(ctx, notifyApps, client)
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

func TestFindOutdatedApps(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	bpFilename := "python_buildpack-cflinuxfs4-v1.2.3.zip"

	apps := []appSpec{
		{guid: "started-outdated", name: "started-outdated", spaceGUID: "space-1"},
		{guid: "started-current", name: "started-current", spaceGUID: "space-1"},
		{guid: "started-unsupported", name: "started-unsupported", spaceGUID: "space-1"},
		{guid: "started-missing-filename", name: "started-missing-filename", spaceGUID: "space-1"},
		{guid: "invalid-git-reference", name: "invalid-git-reference", spaceGUID: "space-1"},
		{guid: "outdated-pinned-git-version", name: "outdated-pinned-git-version", spaceGUID: "space-1"},
		{guid: "current-pinned-git-version", name: "current-pinned-git-version", spaceGUID: "space-1"},
		{guid: "newer-pinned-git-version", name: "newer-pinned-git-version", spaceGUID: "space-1"},
	}

	client, closeFn := newV3TestServerAndClient(t, apps, map[string]spaceSpec{
		"space-1": {name: "dev", orgName: "sandbox"},
	}, nil, []buildpackSpec{
		{guid: "bp-1", name: "python_buildpack", updatedAt: newer, filename: &bpFilename},
		{guid: "bp-2", name: "ruby_buildpack", updatedAt: newer, filename: nil},
	}, map[string]dropletSpec{
		"started-outdated": {
			createdAt:  older,
			buildpacks: []resource.DetectedBuildpack{{Name: "python_buildpack", Version: "1.2.2"}},
		},
		"started-current": {
			createdAt:  newer.Add(24 * time.Hour),
			buildpacks: []resource.DetectedBuildpack{{Name: "python_buildpack", Version: "1.2.3"}},
		},
		"started-unsupported": {
			createdAt:  older,
			buildpacks: []resource.DetectedBuildpack{{Name: "custom_buildpack"}},
		},
		"started-missing-filename": {
			createdAt:  older,
			buildpacks: []resource.DetectedBuildpack{{Name: "ruby_buildpack"}},
		},
		"invalid-git-reference": {
			buildpacks: []resource.DetectedBuildpack{{Name: "https://gitlab.com/cloudfoundry/python-buildpack.git#v1.2.3"}},
		},
		"outdated-pinned-git-version": {
			buildpacks: []resource.DetectedBuildpack{{Name: "https://github.com/cloudfoundry/python-buildpack.git#v1.2.2"}},
		},
		"current-pinned-git-version": {
			buildpacks: []resource.DetectedBuildpack{{Name: "https://github.com/cloudfoundry/python-buildpack.git#v1.2.4"}},
		},
		"newer-pinned-git-version": {
			buildpacks: []resource.DetectedBuildpack{{Name: "https://github.com/cloudfoundry/python-buildpack.git#v1.2.5"}},
		},
	})
	defer closeFn()

	ctx := context.Background()
	cfApps, err := client.Applications.ListAll(ctx, nil)
	if err != nil {
		t.Fatalf("listing apps: %v", err)
	}
	for idx := range cfApps {
		cfApps[idx].State = "STARTED"
	}
	cfApps = append(cfApps, &resource.App{Name: "stopped-app", State: "STOPPED", Resource: resource.Resource{GUID: "stopped-app"}})

	buildpackList, err := client.Buildpacks.ListAll(ctx, nil)
	if err != nil {
		t.Fatalf("listing buildpacks: %v", err)
	}
	buildpacks := map[string]resource.Buildpack{}
	for _, bp := range buildpackList {
		buildpacks[bp.Name] = *bp
	}

	outdatedApps, updatedBuildpacks := findOutdatedApps(ctx, client, cfApps, buildpacks)
	assert.Equal(t, 2, len(outdatedApps))
	assert.Equal(t, "started-outdated", outdatedApps[0].Name)
	assert.Equal(t, "outdated-pinned-git-version", outdatedApps[1].Name)
	assert.Equal(t, 2, len(updatedBuildpacks))
	assert.Equal(t, "python_buildpack", updatedBuildpacks[0].BuildpackName)
	assert.Equal(t, "v1.2.3", updatedBuildpacks[0].BuildpackVersion)
	assert.Equal(t, "python_buildpack", updatedBuildpacks[1].BuildpackName)
	assert.Equal(t, "v1.2.3", updatedBuildpacks[1].BuildpackVersion)
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
						updatedBuildpacks,
					},
					"Action required: restage your application(s)",
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
						updatedBuildpacks,
					},
					"Action required: restage your application(s)",
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
						updatedBuildpacks,
					},
					"Action required: restage your application(s)",
				},
				{
					notifyEmail{
						"bob@example.com",
						[]notifyApp{
							{Name: "testapp2"},
						},
						updatedBuildpacks,
					},
					"Action required: restage your application(s)",
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
						updatedBuildpacks,
					},
					"Action required: restage your application(s)",
				},
				{
					notifyEmail{
						"bob@example.com",
						[]notifyApp{
							{Name: "testapp3"},
							{Name: "testapp4"},
						},
						updatedBuildpacks,
					},
					"Action required: restage your application(s)",
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
