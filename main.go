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
	defer fp.Close()
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
	defer func(in *os.File) {
		err := in.Close()
		if err != nil {
			log.Fatalf("Error closing inPath %s err: %s", inPath, err)
		}
	}(in)

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func(out *os.File) {
		err := out.Close()
		if err != nil {
			log.Fatalf("Error closing outPath %s err: %s", outPath, err)
		}
	}(out)
	_, err = io.Copy(out, in)
	return err
}

func saveState(state map[string]buildpackRecord, path string) error {
	fp, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func(fp *os.File) {
		err := fp.Close()
		if err != nil {
			log.Fatalf("Error closing path %s err: %s", path, err)

		}
	}(fp)
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
	var (
		cfg *cfconfig.Config
	)
	if os.Getenv("INSECURE") == "1" {
		cfg, _ = cfconfig.New(cfAPIConfig.API, cfconfig.ClientCredentials(cfAPIConfig.ClientID, cfAPIConfig.ClientSecret),
			cfconfig.HttpClient(&http.Client{Timeout: 30 * time.Second}), cfconfig.SkipTLSValidation())
	} else {
		cfg, _ = cfconfig.New(cfAPIConfig.API, cfconfig.ClientCredentials(cfAPIConfig.ClientID, cfAPIConfig.ClientSecret),
			cfconfig.HttpClient(&http.Client{Timeout: 30 * time.Second}))
	}
	client, err := cfclient.New(cfg)
	if err != nil {
		log.Fatalf("Unable to create client. Error: %s", err.Error())
	}
	log.Println("Calculating notifications to send for outdated buildpacks.")
	mailer := InitSMTPMailer(emailConfig)
	apps, buildpacks, state := getAppsAndBuildpacks(client, ctx, state)
	outdatedApps, updatedBuildpacks := findOutdatedApps(client, apps, buildpacks)
	//outdatedV2Apps := convertToV2Apps(client, outdatedApps)
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
	// 2) If it is in state and UpdatedAt is newer than what we stored -> keep it, update state.
	// 3) Otherwise it's unchanged -> skip.

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

//func filterForNewlyUpdatedBuildpacks(buildpacks []resource.Buildpack, state map[string]buildpackRecord) ([]resource.Buildpack, map[string]buildpackRecord) {
//	filteredBuildpacks := []resource.Buildpack{}
//	// Go through the passed in buildpacks
//	// Check if current buildpack.guid matches a guid in storeBuildpacks
//	// 1) If so, compare the buildpack.Meta.UpdatedAt with the storeBuildpack.LastUpdatedAt
//	// 1a)   If buildpack.Meta.UpdatedAt (updated recently) > storeBuildpack.LastUpdatedAt,
//	//       then add to filteredBuildpacks and updated database
//	// 1b)   Else, continue
//	// 2) If not, add to filteredBuildpacks and updated database
//	// for buildpacks return buildpack.guid in stored.
//
//	for _, buildpack := range buildpacks {
//		storedBuildpack, found := state[buildpack.GUID]
//		if !found {
//			filteredBuildpacks = append(filteredBuildpacks, buildpack)
//			state[buildpack.GUID] = buildpackRecord{LastUpdatedAt: buildpack.UpdatedAt}
//		} else {
//			buildpackUpdatedAt, err := time.Parse(time.RFC3339, buildpack.UpdatedAt)
//			if err != nil {
//				log.Fatalf("Unable to parse buildpack updatedAt time. Buildpack GUID %s Error %s",
//					buildpack.GUID, err)
//			}
//			storedBuildpackUpdatedAt, err := time.Parse(time.RFC3339, storedBuildpack.LastUpdatedAt)
//			if err != nil {
//				log.Fatalf("Unable to parse stored buildpack LastUpdatedAt time. Buildpack GUID %s Error %s",
//					buildpack.GUID, err)
//			}
//			if buildpackUpdatedAt.After(storedBuildpackUpdatedAt) {
//				filteredBuildpacks = append(filteredBuildpacks, buildpack)
//				state[buildpack.GUID] = buildpackRecord{LastUpdatedAt: buildpack.UpdatedAt}
//			} else {
//				log.Printf("Supported Buildpack %s has not been updated\n", buildpack.Name)
//				continue
//			}
//		}
//
//	}
//
//	return filteredBuildpacks, state
//}

func getAppsAndBuildpacks(client *cfclient.Client, ctx context.Context, state map[string]buildpackRecord) ([]*resource.App, map[string]resource.Buildpack, map[string]buildpackRecord) {
	//apps, err := ListApps(client)
	apps, err := client.Applications.ListAll(ctx, nil)
	if err != nil {
		log.Fatalf("Unable to get apps. Error: %s", err.Error())
	}
	// Get all the buildpacks from our CF deployment via CF_API.
	buildpackList, err := client.Buildpacks.ListAll(ctx, nil)
	if err != nil {
		log.Fatalf("Unable to get buildpacks. Error: %s", err)
	}
	filteredBuildpackList, state := filterForNewlyUpdatedBuildpacks(buildpackList, state)

	// Create a map with the key being the buildpack name for quick comparison later on.
	buildpacks := make(map[string]resource.Buildpack)
	for _, buildpack := range filteredBuildpackList {
		buildpacks[buildpack.Name] = buildpack
	}
	return apps, buildpacks, state
}

func deduplicateBuildpacks(allBuildpacks []buildpackReleaseInfo) []buildpackReleaseInfo {
	keys := make(map[buildpackReleaseInfo]bool)
	deduplicated := []buildpackReleaseInfo{}
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

// isDropletUsingOutdatedBuildpack checks if the droplet was created before the last time the buildpack was updated.
// This comparison is the heart of checking whether the app needs an update.
// Format of time stamp: 2016-06-08T16:41:45Z
//func isDropletUsingOutdatedBuildpack(client *cfclient.Client, droplet Droplet, buildpack *resource.Buildpack) bool {
//	timeOfLastAppRestage, err := time.Parse(time.RFC3339, droplet.CreatedAt)
//	if err != nil {
//		log.Fatalf("Unable to parse last restage time. Droplet GUID %s Error %s",
//			droplet.GUID, err)
//	}
//timeOfLastBuildpackUpdate, err := time.Parse(
//	time.RFC3339,
//	buildpack.UpdatedAt.Format("2006-01-02 15:04:05"),
//)
//timeOfLastBuildpackUpdate := buildpack.UpdatedAt
//if err != nil {
//	log.Fatalf("Unable to parse last buildpack update time. Buildpack %s Buildpack GUID %s Error %s",
//		buildpack.Name, buildpack.Guid, err)
//}
//return timeOfLastBuildpackUpdate.After(timeOfLastAppRestage)
// }

//type cfSpaceCache struct {
//	spaceUsers map[string]map[string]cfclient.SpaceRole
//}
//
//func createCFSpaceCache() *cfSpaceCache {
//	return &cfSpaceCache{
//		spaceUsers: make(map[string]map[string]cfclient.SpaceRole),
//	}
//}

//	func filterForValidEmailUsernames(users []cfclient.SpaceRole, app cfclient.App) []cfclient.SpaceRole {
//		var filteredUsers []cfclient.SpaceRole
//		for _, user := range users {
//			if _, err := mail.ParseAddress(user.Username); err == nil {
//				filteredUsers = append(filteredUsers, user)
//			} else {
//				log.Printf("Dropping notification to user %s about app %s in space %s because "+
//					"invalid e-mail address\n", user.Username, app.Name, app.SpaceGuid)
//			}
//		}
//		return filteredUsers
//	}
func filterForValidEmailUsernames(users []*resource.User, app *resource.App) []*resource.User {
	var filteredUsers []*resource.User
	for _, user := range users {
		if _, err := mail.ParseAddress(*user.Username); err != nil {
			log.Printf("Dropping notification to user %s about app %s in space %s because "+
				"invalid e-mail address\n", *user.Username, app.Name, app.Relationships.Space.Data.GUID)
		} else {
			filteredUsers = append(filteredUsers, user)
		}
	}
	return filteredUsers
}

//func (c *cfSpaceCache) getOwnersInAppSpace(app resource.App, client *cfclient.Client) map[string]cfclient.SpaceRole {
//	var ok bool
//	var ownersWithSpaceRoles map[string]cfclient.SpaceRole
//	if ownersWithSpaceRoles, ok = c.spaceUsers[app.SpaceGuid]; ok {
//		return ownersWithSpaceRoles
//	}
//	space, err := app.Space()
//	if err != nil {
//		log.Fatalf("Unable to get space of app %s. Error: %s", app.Name, err.Error())
//	}
//	spaceRoles, err := space.Roles()
//	if err != nil {
//		log.Fatalf("Unable to get roles for all users in space %s. Error: %s", space.Name, err.Error())
//	}
//	spaceRoles = filterForValidEmailUsernames(spaceRoles, app)
//	ownersWithSpaceRoles = filterForUsersWithRoles(spaceRoles, getAppOwnerRoles())
//
//	c.spaceUsers[app.SpaceGuid] = ownersWithSpaceRoles
//
//	return ownersWithSpaceRoles
//}

// Returns a map of space roles we consider to be an owner.
// We return a map for quick look-ups and comparisons.
//func getAppOwnerRoles() map[string]bool {
//	return map[string]bool{
//		"space_manager":   true,
//		"space_developer": true,
//	}
//}

//	func filterForUsersWithRoles(spaceUsers []cfclient.SpaceRole, filteredRoles map[string]bool) map[string]cfclient.SpaceRole {
//		filteredSpaceUsers := make(map[string]cfclient.SpaceRole)
//		for _, spaceUser := range spaceUsers {
//			if spaceUserHasRoles(spaceUser, filteredRoles) {
//				filteredSpaceUsers[spaceUser.Guid] = spaceUser
//			}
//		}
//		return filteredSpaceUsers
//	}
type cfSpaceCache struct {
	// spaceGUID -> owner users (space_manager/space_developer) with valid-email usernames
	spaceUsers map[string][]*resource.User
}

func createCFSpaceCache() *cfSpaceCache {
	return &cfSpaceCache{
		spaceUsers: make(map[string][]*resource.User),
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
) []*resource.User {
	spaceGUID := app.Relationships.Space.Data.GUID

	if users, ok := c.spaceUsers[spaceGUID]; ok {
		return users
	}

	opts := cfclient.NewRoleListOptions()
	opts.SpaceGUIDs.EqualTo(spaceGUID)
	// Replaces v2 getAppOwnerRoles() + filterForUsersWithRoles():
	// let CF return only the owner role types.
	opts.WithSpaceRoleType(getAppOwnerSpaceRoleTypes()...)

	_, users, err := client.Roles.ListIncludeUsersAll(ctx, opts)
	if err != nil {
		log.Fatalf("Unable to get roles for all users in space %s. Error: %s", spaceGUID, err.Error())
	}

	ownersWithSpaceRoles := filterForValidEmailUsernames(users, app)

	c.spaceUsers[spaceGUID] = ownersWithSpaceRoles
	return ownersWithSpaceRoles
}

func findOwnersOfApps(ctx context.Context, apps []*resource.App, client *cfclient.Client) map[string][]resource.App {
	// Mapping of users to the apps.
	owners := make(map[string][]resource.App)
	spaceCache := createCFSpaceCache()
	for _, app := range apps {
		// Get the space
		ownersWithSpaceRoles := spaceCache.getOwnersInAppSpace(ctx, app, client)
		for _, ownerWithSpaceRoles := range ownersWithSpaceRoles {
			owners[*ownerWithSpaceRoles.Username] = append(owners[*ownerWithSpaceRoles.Username], *app)
		}
	}
	return owners
}

// getCurrentDropletForApp will try to query the current droplet.
// A running app will have 1 droplet associated with it.
// If it doesn't have 1, it's not running. There should be no case when it's more
// than 1 but if so, we need to do further investigation to handle it.
//func getCurrentDropletForApp(app *resource.App, client *cfclient.Client) (Droplet, bool) {
//	droplets, err := app.GetDropletsByQuery(client, url.Values{"current": []string{"true"}})
//	if err != nil {
//		// Log and continue if droplet not found
//		log.Printf("Unable to get droplet for app. App %s App GUID %s Error %s",
//			app.Name, app.GUID, err)
//	}
//	if len(droplets) != 1 {
//		// We should only have 1.
//		return Droplet{}, false
//	}
//	return droplets[0], true
// }

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

func findOutdatedApps(client *cfclient.Client, apps []*resource.App, buildpacks map[string]resource.Buildpack) (outdatedApps []*resource.App, updatedBuildpacks []buildpackReleaseInfo) {
	for _, app := range apps {
		if app.State != "STARTED" {
			log.Printf("App %s guid %s not in STARTED state\n", app.Name, app.GUID)
			continue
		}
		ctx := context.Background()
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

func spaceUserHasRoles(user cfclient.SpaceRole, roles map[string]bool) bool {
	for _, roleOfUser := range user.SpaceRoles {
		if found, _ := roles[roleOfUser]; found {
			return true
		}
	}
	return false
}

func sendNotifyEmailToUsers(users map[string][]cfclient.App, updatedBuildpacks []buildpackReleaseInfo, templates *Templates, mailer Mailer, dryRun bool) {
	for user, apps := range users {
		// Create buffer
		body := new(bytes.Buffer)
		// Determine whether the user has one application or more than one.
		isMultipleApp := false
		if len(apps) > 1 {
			isMultipleApp = true
		}
		// Fill buffer with completed e-mail
		err := templates.getNotifyEmail(body, notifyEmail{user, apps, isMultipleApp, updatedBuildpacks})
		if err != nil {
			log.Fatalf("Error getNotifyEmail for user %s err: %s", user, err)
			return
		}
		// Send email
		if !dryRun {
			subj := "Action required: restage your application"
			if isMultipleApp {
				subj += "s"
			}
			err := mailer.SendEmail(user, fmt.Sprint(subj), body.Bytes())
			if err != nil {
				log.Printf("Unable to send e-mail to %s\n", user)
				continue
			}
		}
		fmt.Printf("Sent e-mail to %s\n", user)
	}
}
