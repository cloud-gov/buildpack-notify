package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"

	cfclient "github.com/cloudfoundry/go-cfclient/v3/client"
	cfconfig "github.com/cloudfoundry/go-cfclient/v3/config"
	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"github.com/kelseyhightower/envconfig"
)

// TODO: handle errors centrally.

type Config struct {
	InState  string `envconfig:"in_state" required:"true"`
	OutState string `envconfig:"out_state" required:"true"`
	DryRun   bool   `envconfig:"dry_run"`
}

type EmailConfig struct {
	From     string `envconfig:"smtp_from" required:"true"`
	Host     string `envconfig:"smtp_host" required:"true"`
	Password string `envconfig:"smtp_password" required:"true"`
	Port     string `envconfig:"smtp_port" required:"true"`
	User     string `envconfig:"smtp_user" required:"true"`
	Cert     string `envconfig:"smtp_cert"`
}

type CFAPIConfig struct {
	API          string `envconfig:"cf_api" required:"true"`
	ClientID     string `envconfig:"client_id" required:"true"`
	ClientSecret string `envconfig:"client_secret" required:"true"`
}

type buildpackRecord struct {
	LastUpdatedAt time.Time
}

type buildpackReleaseInfo struct {
	BuildpackName    string
	BuildpackVersion string
	BuildpackURL     string
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

func parseBuildpackVersion(buildpackFileName string) string {
	// Takes a buildpack file name and parses out the version number from it.
	// Buildpack filenames currently look like this: python_buildpack-cflinuxfs3-v1.7.43.zip
	// "v1.7.43" is the version in this case.

	fileNameParts := strings.Split(buildpackFileName, "-")
	buildpackVersion := strings.ReplaceAll(fileNameParts[len(fileNameParts)-1], ".zip", "")
	return buildpackVersion
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

func loadState(path string) (map[string]buildpackRecord, error) {
	fp, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := fp.Close(); err != nil {
			log.Printf("Error closing load state at path %s: %s", path, err)
		}
	}()
	decoder := json.NewDecoder(fp)
	var state map[string]buildpackRecord
	if err := decoder.Decode(&state); err != nil {
		return nil, err
	}
	return state, nil
}

func copyState(inPath, outPath string) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := in.Close(); err != nil {
			log.Printf("Error closing input state path %s: %s", inPath, err)
		}
	}()

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := out.Close(); err != nil {
			log.Printf("Error closing output state path %s: %s", outPath, err)
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

func saveState(state map[string]buildpackRecord, path string) error {
	fp, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := fp.Close(); err != nil {
			log.Printf("Error closing state path %s: %s", path, err)
		}
	}()
	encoder := json.NewEncoder(fp)
	return encoder.Encode(state)
}

func main() {
	var (
		config      Config
		emailConfig EmailConfig
		cfAPIConfig CFAPIConfig
	)
	ctx := context.Background()

	if err := envconfig.Process("", &config); err != nil {
		log.Fatalf("Unable to parse config: %s", err.Error())
	}
	if err := envconfig.Process("", &emailConfig); err != nil {
		log.Fatalf("Unable to parse email config: %s", err.Error())
	}
	if err := envconfig.Process("", &cfAPIConfig); err != nil {
		log.Fatalf("Unable to parse cf api config: %s", err.Error())
	}

	if config.DryRun {
		log.Println("Dry-Run mode activated. No modifications happening")
	}

	state, err := loadState(config.InState)
	if err != nil {
		log.Fatalf("Error reading state: %s", err)
	}

	templates, err := initTemplates()
	if err != nil {
		log.Fatalf("Unable to initialize templates: %s", err)
	}
	cfg, err := cfconfig.New(cfAPIConfig.API, cfconfig.ClientCredentials(cfAPIConfig.ClientID, cfAPIConfig.ClientSecret),
		cfconfig.HttpClient(&http.Client{Timeout: 30 * time.Second}))
	if err != nil {
		log.Fatalf("Unable to create config. Error: %s", err.Error())
	}
	client, err := cfclient.New(cfg)
	if err != nil {
		log.Fatalf("Unable to create client. Error: %s", err.Error())
	}
	log.Println("Calculating notifications to send for outdated buildpacks.")
	mailer := InitSMTPMailer(emailConfig)
	apps, buildpacks, state, err := getAppsAndBuildpacks(client, ctx, state)
	if err != nil {
		log.Fatalf("Unable to load apps and buildpacks: %s", err)
	}
	outdatedApps, updatedBuildpacks := findOutdatedApps(ctx, client, apps, buildpacks)
	owners := findOwnersOfApps(ctx, outdatedApps, client)
	log.Printf("Will notify %d owners of outdated apps.\n", len(owners))
	updatedBuildpacks = deduplicateBuildpacks(updatedBuildpacks)
	sendNotifyEmailToUsers(owners, updatedBuildpacks, templates, mailer, config.DryRun)

	if config.DryRun {
		if err := copyState(config.InState, config.OutState); err != nil {
			log.Fatalf("Error copying state: %s", err)
		}
	} else {
		if err := saveState(state, config.OutState); err != nil {
			log.Fatalf("Error saving state: %s", err)
		}
	}
}

func filterForNewlyUpdatedBuildpacks(
	buildpacks []*resource.Buildpack,
	state map[string]buildpackRecord,
) ([]resource.Buildpack, map[string]buildpackRecord) {
	var filteredBuildpacks []resource.Buildpack

	// For each buildpack:
	// 1) If its GUID isn't in state -> it's new, keep it and record its UpdatedAt.
	// 2) If it is in state and UpdatedAt is newer than what we stored, keep it and update state.
	// 3) Otherwise it's unchanged, skip it.

	for _, buildpack := range buildpacks {
		storedBuildpack, found := state[buildpack.GUID]

		if !found || buildpack.UpdatedAt.After(storedBuildpack.LastUpdatedAt) {
			filteredBuildpacks = append(filteredBuildpacks, *buildpack)
			state[buildpack.GUID] = buildpackRecord{LastUpdatedAt: buildpack.UpdatedAt}
			continue
		}

		log.Printf("Supported Buildpack %s has not been updated\n", buildpack.Name)
	}

	return filteredBuildpacks, state
}

func getAppsAndBuildpacks(client *cfclient.Client, ctx context.Context, state map[string]buildpackRecord) ([]*resource.App, map[string]resource.Buildpack, map[string]buildpackRecord, error) {
	apps, err := client.Applications.ListAll(ctx, nil)
	if err != nil {
		return nil, nil, state, err
	}
	// Get all the buildpacks from our CF deployment via CF_API.
	buildpackList, err := client.Buildpacks.ListAll(ctx, nil)
	if err != nil {
		return nil, nil, state, err
	}
	filteredBuildpackList, state := filterForNewlyUpdatedBuildpacks(buildpackList, state)

	// Create a map with the key being the buildpack name for quick comparison later on.
	buildpacks := make(map[string]resource.Buildpack)
	for _, buildpack := range filteredBuildpackList {
		buildpacks[buildpack.Name] = buildpack
	}
	return apps, buildpacks, state, nil
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
func isDropletUsingSupportedBuildpack(droplet resource.Droplet, buildpacks map[string]resource.Buildpack) (bool, *resource.Buildpack) {
	for _, dropletBuildpack := range droplet.Buildpacks {
		if buildpack, found := buildpacks[dropletBuildpack.Name]; found && dropletBuildpack.Name != "" {
			return true, &buildpack
		}
	}
	return false, nil
}

// filterForValidEmailUsernames drops any users whose username is not a valid
// e-mail address, since notifications are delivered by e-mail.
func filterForValidEmailUsernames(users []*resource.User, app *resource.App) []*resource.User {
	var filteredUsers []*resource.User
	for _, user := range users {
		if user.Username == nil {
			log.Printf("Dropping notification to user %s about app %s in space %s because missing e-mail address\n",
				user.GUID, app.Name, app.Relationships.Space.Data.GUID)
			continue
		}
		if _, err := mail.ParseAddress(*user.Username); err != nil {
			log.Printf("Dropping notification to user %s about app %s in space %s because "+
				"invalid e-mail address\n", *user.Username, app.Name, app.Relationships.Space.Data.GUID)
		} else {
			filteredUsers = append(filteredUsers, user)
		}
	}
	return filteredUsers
}

type cfSpaceCache struct {
	// spaceGUID -> owner users (space_manager/space_developer) with valid-email usernames
	spaceUsers map[string][]*resource.User
	// spaceGUID -> resolved space and org names, used to render the email to notify users.
	spaceNames map[string]spaceOrgNames
}

type spaceOrgNames struct {
	SpaceName string
	OrgName   string
}

func createCFSpaceCache() *cfSpaceCache {
	return &cfSpaceCache{
		spaceUsers: make(map[string][]*resource.User),
		spaceNames: make(map[string]spaceOrgNames),
	}
}

func getAppOwnerSpaceRoleTypes() []resource.SpaceRoleType {
	return []resource.SpaceRoleType{
		resource.SpaceRoleManager,
		resource.SpaceRoleDeveloper,
	}
}

func (c *cfSpaceCache) getOwnersInAppSpace(
	ctx context.Context,
	app *resource.App,
	client *cfclient.Client,
) ([]*resource.User, error) {
	spaceGUID := app.Relationships.Space.Data.GUID

	if users, ok := c.spaceUsers[spaceGUID]; ok {
		return users, nil
	}

	opts := cfclient.NewRoleListOptions()
	opts.SpaceGUIDs.EqualTo(spaceGUID)
	// Replaces v2 getAppOwnerRoles() + filterForUsersWithRoles():
	// let CF return only the owner role types.
	opts.WithSpaceRoleType(getAppOwnerSpaceRoleTypes()...)

	_, users, err := client.Roles.ListIncludeUsersAll(ctx, opts)
	if err != nil {
		return nil, err
	}

	ownersWithSpaceRoles := filterForValidEmailUsernames(users, app)

	c.spaceUsers[spaceGUID] = ownersWithSpaceRoles
	return ownersWithSpaceRoles, nil
}

// getSpaceOrgNames resolves (and caches) the space name and parent org name for
// an app's space. v3 resource.App only carries the space GUID, so we look these
// up to render the "cf target -o ORG -s SPACE" line in the email to notify users.
func (c *cfSpaceCache) getSpaceOrgNames(
	ctx context.Context,
	app *resource.App,
	client *cfclient.Client,
) (spaceOrgNames, error) {
	spaceGUID := app.Relationships.Space.Data.GUID

	if names, ok := c.spaceNames[spaceGUID]; ok {
		return names, nil
	}

	space, org, err := client.Spaces.GetIncludeOrganization(ctx, spaceGUID)
	if err != nil {
		return spaceOrgNames{}, err
	}

	names := spaceOrgNames{SpaceName: space.Name, OrgName: org.Name}
	c.spaceNames[spaceGUID] = names
	return names, nil
}

func findOwnersOfApps(ctx context.Context, apps []*resource.App, client *cfclient.Client) map[string][]notifyApp {
	// Mapping of users to the apps.
	owners := make(map[string][]notifyApp)
	spaceCache := createCFSpaceCache()
	for _, app := range apps {
		ownersWithSpaceRoles, err := spaceCache.getOwnersInAppSpace(ctx, app, client)
		if err != nil {
			log.Printf("Unable to get owners for app %s guid %s in space %s: %s\n",
				app.Name, app.GUID, app.Relationships.Space.Data.GUID, err)
			continue
		}
		if len(ownersWithSpaceRoles) == 0 {
			continue
		}
		names, err := spaceCache.getSpaceOrgNames(ctx, app, client)
		if err != nil {
			log.Printf("Unable to get org/space names for app %s guid %s in space %s: %s\n",
				app.Name, app.GUID, app.Relationships.Space.Data.GUID, err)
			continue
		}
		appView := notifyApp{
			Name:      app.Name,
			SpaceName: names.SpaceName,
			OrgName:   names.OrgName,
		}
		for _, ownerWithSpaceRoles := range ownersWithSpaceRoles {
			owners[*ownerWithSpaceRoles.Username] = append(owners[*ownerWithSpaceRoles.Username], appView)
		}
	}
	return owners
}

// getCurrentDropletForApp will try to query the current droplet.
// A running app will have 1 droplet associated with it.
// If it doesn't have 1, it's not running.
func getCurrentDropletForApp(
	ctx context.Context,
	app resource.App,
	client *cfclient.Client,
) (*resource.Droplet, bool) {
	droplet, err := client.Droplets.GetCurrentForApp(ctx, app.GUID)
	if err != nil {
		log.Printf(
			"Unable to get current droplet for app. App %s App GUID %s Error %s",
			app.Name,
			app.GUID,
			err,
		)
		return nil, false
	}

	return droplet, true
}

func isDropletUsingOutdatedBuildpack(
	droplet *resource.Droplet,
	buildpack *resource.Buildpack,
) bool {
	if droplet == nil || buildpack == nil {
		return false
	}

	return droplet.CreatedAt.Before(buildpack.UpdatedAt)
}

func findOutdatedApps(ctx context.Context, client *cfclient.Client, apps []*resource.App, buildpacks map[string]resource.Buildpack) (outdatedApps []*resource.App, updatedBuildpacks []buildpackReleaseInfo) {
	for _, app := range apps {
		if app.State != "STARTED" {
			log.Printf("App %s guid %s not in STARTED state\n", app.Name, app.GUID)
			continue
		}
		droplet, foundDroplet := getCurrentDropletForApp(ctx, *app, client)
		if !foundDroplet {
			log.Printf("Unable to find current droplet for app %s guid %s. Safely skipping.\n", app.Name, app.GUID)
			continue
		}
		yes, buildpack := isDropletUsingSupportedBuildpack(*droplet, buildpacks)
		if !yes {
			log.Printf("App %s guid %s not using supported buildpack\n", app.Name, app.GUID)
			continue
		}
		// If the app is using a supported buildpack, check if app is using an outdated buildpack.
		if appIsOutdated := isDropletUsingOutdatedBuildpack(droplet, buildpack); !appIsOutdated {
			log.Printf("App %s Guid %s | Buildpack %s not outdated\n", app.Name, app.GUID, buildpack.Name)
			continue
		} else {
			// If the app is using an outdated buildpack, get the buildpack information to pass along to the user.
			log.Printf("App %s Guid %s | Buildpack %s is outdated\n", app.Name, app.GUID, buildpack.Name)
			buildpackReleaseURL := getBuildpackReleaseURL(buildpack.Name)
			if buildpack.Filename == nil {
				log.Printf("Buildpack %s guid %s missing filename metadata; skipping release info\n", buildpack.Name, buildpack.GUID)
				continue
			}
			buildpackVersion := parseBuildpackVersion(*buildpack.Filename)
			buildpackVersionURL := getBuildpackVersionURL(buildpackReleaseURL, buildpackVersion)

			updatedBuildpack := buildpackReleaseInfo{
				BuildpackName:    buildpack.Name,
				BuildpackVersion: buildpackVersion,
				BuildpackURL:     buildpackVersionURL,
			}

			updatedBuildpacks = append(updatedBuildpacks, updatedBuildpack)
		}
		outdatedApps = append(outdatedApps, app)
	}
	return
}

func sendNotifyEmailToUsers(users map[string][]notifyApp, updatedBuildpacks []buildpackReleaseInfo, templates *Templates, mailer Mailer, dryRun bool) {
	for user, apps := range users {
		// Create buffer
		body := new(bytes.Buffer)
		// Determine whether the user has one application or more than one.
		isMultipleApp := len(apps) > 1
		// Fill buffer with completed e-mail
		err := templates.getNotifyEmail(body, notifyEmail{user, apps, isMultipleApp, updatedBuildpacks})
		if err != nil {
			log.Printf("Unable to render e-mail for %s: %s\n", user, err)
			continue
		}
		// Send email
		if !dryRun {
			subj := "Action required: restage your application"
			if isMultipleApp {
				subj += "s"
			}
			err := mailer.SendEmail(user, subj, body.Bytes())
			if err != nil {
				log.Printf("Unable to send e-mail to %s\n", user)
				continue
			}
		}
		fmt.Printf("Sent e-mail to %s\n", user)
	}
}
