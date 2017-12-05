package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/lib/pq"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	cfenv "github.com/cloudfoundry-community/go-cfenv"
)

// TODO: handle errors centrally.

const (
	cfUPSCreds    = "notify-cf-creds"
	emailUPSCreds = "notify-email-creds"
)

type emailConfig struct {
	from     string
	host     string
	password string
	port     string
	user     string
}

type cfConfig struct {
	api          string
	clientID     string
	clientSecret string
}

type dbConfig struct {
	databaseURL string
}

type notifyStore interface {
	GetBuildpacks() map[string]buildpackRecord
	SaveBuildpack(*buildpackRecord)
}

type dbNotifyStore struct {
	db     *gorm.DB
	dryRun bool
}

func newDBNotifyStore(db *gorm.DB, clear, dryRun bool) notifyStore {
	if clear {
		log.Println("Dropping tables...")
		db.DropTableIfExists(&buildpackRecord{})
	}
	db.AutoMigrate(&buildpackRecord{})
	return &dbNotifyStore{db: db, dryRun: dryRun}
}

func (s *dbNotifyStore) GetBuildpacks() map[string]buildpackRecord {
	buildpacks := []buildpackRecord{}
	err := s.db.Find(&buildpacks).Error
	if err != nil {
		log.Fatalf("Unable to get buildpack records from notify database. Error: %s", err.Error())
	}
	m := make(map[string]buildpackRecord, len(buildpacks))
	for _, buildpack := range buildpacks {
		m[buildpack.Guid] = buildpack
	}
	return m
}

func (s *dbNotifyStore) SaveBuildpack(buildpack *buildpackRecord) {
	log.Printf("Saving / updating buildpack %s", buildpack.Guid)
	if s.dryRun {
		return
	}
	newRecord := s.db.NewRecord(buildpack)
	var err error
	if newRecord {
		err = s.db.Create(buildpack).Error
	} else {
		err = s.db.Save(buildpack).Error
	}
	if err != nil {
		log.Fatalf("Unable to save record for buildpack. Guid: %s. Error %s", buildpack.Guid, err.Error())
	}

}

func dbConnect(dbConfig dbConfig) (*gorm.DB, error) {
	return gorm.Open("postgres", dbConfig.databaseURL)
}

func getDBConfig() dbConfig {
	return dbConfig{
		databaseURL: os.Getenv("DATABASE_URL"),
	}
}

func getCFConfig(cfEnv *cfenv.App) cfConfig {
	config := cfConfig{
		api:          os.Getenv("CF_API"),
		clientID:     os.Getenv("CLIENT_ID"),
		clientSecret: os.Getenv("CLIENT_SECRET"),
	}
	if cfEnv != nil {
		if service, err := cfEnv.Services.WithName(cfUPSCreds); err == nil {
			log.Println("Using UPS for CF creds")
			if api, found := service.Credentials["CF_API"]; found {
				config.api = api.(string)
			}
			if clientID, found := service.Credentials["CLIENT_ID"]; found {
				config.clientID = clientID.(string)
			}
			if clientSecret, found := service.Credentials["CLIENT_SECRET"]; found {
				config.clientSecret = clientSecret.(string)
			}
		}
	}
	return config
}

func getEmailConfig(cfEnv *cfenv.App) emailConfig {
	config := emailConfig{
		from:     os.Getenv("SMTP_FROM"),
		host:     os.Getenv("SMTP_HOST"),
		password: os.Getenv("SMTP_PASS"),
		port:     os.Getenv("SMTP_PORT"),
		user:     os.Getenv("SMTP_USER"),
	}
	if cfEnv != nil {
		if service, err := cfEnv.Services.WithName(emailUPSCreds); err == nil {
			log.Println("Using UPS for email creds")
			if smtpFrom, found := service.Credentials["SMTP_FROM"]; found {
				config.from = smtpFrom.(string)
			}
			if smtpHost, found := service.Credentials["SMTP_HOST"]; found {
				config.host = smtpHost.(string)
			}
			if smtpPass, found := service.Credentials["SMTP_PASS"]; found {
				config.password = smtpPass.(string)
			}
			if smtpPort, found := service.Credentials["SMTP_PORT"]; found {
				config.port = smtpPort.(string)
			}
			if smtpUser, found := service.Credentials["SMTP_USER"]; found {
				config.user = smtpUser.(string)
			}
		}
	}
	return config
}

func main() {
	cfEnv, err := cfenv.Current()
	if err != nil {
		log.Println("Could not find cf env")
	}
	config := getEmailConfig(cfEnv)
	dbConfig := getDBConfig()
	cfAPIConfig := getCFConfig(cfEnv)
	db, err := dbConnect(dbConfig)
	if err != nil {
		log.Fatalf("Unable to connect to database. Error: %s", err.Error())
	}
	templates, err := initTemplates()
	if err != nil {
		log.Fatalf("Unable to initialize templates. Error: %s", err.Error())
	}
	client, err := cfclient.NewClient(&cfclient.Config{
		ApiAddress:        cfAPIConfig.api,
		ClientID:          cfAPIConfig.clientID,
		ClientSecret:      cfAPIConfig.clientSecret,
		SkipSslValidation: os.Getenv("INSECURE") == "1",
		HttpClient:        &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		log.Fatalf("Unable to create client. Error: %s", err.Error())
	}
	clear := flag.Bool("clear", false, "clear database tables of values")
	notify := flag.Bool("notify", false, "run notification program")
	dryRun := flag.Bool("dry-run", false, "run the program without actual modifications or sending")
	flag.Parse()
	store := newDBNotifyStore(db, *clear, *dryRun)
	if *dryRun {
		log.Println("Dry-Run mode activated. No modifications happening")
	}
	if *notify {
		log.Println("Calculating notifications to send for outdated buildpacks.")
		mailer := InitSMTPMailer(config)
		apps, buildpacks := getAppsAndBuildpacks(client, store)
		outdatedApps := findOutdatedApps(client, apps, buildpacks)
		outdatedV2Apps := convertToV2Apps(client, outdatedApps)
		owners := findOwnersOfApps(outdatedV2Apps, client)
		log.Printf("Will notify %d owners of outdated apps.\n", len(owners))
		sendNotifyEmailToUsers(owners, templates, mailer, *dryRun)
	} else {
		log.Println("Starting notification server.")
		err := http.ListenAndServe(":"+os.Getenv("PORT"), nil)
		if err != nil {
			log.Fatalf("Unable to start server. Error %s", err.Error())
		}
	}
}

type buildpackRecord struct {
	gorm.Model
	Guid          string
	LastUpdatedAt string
}

// convertToV2Apps will take a V3 App object and convert it to a V2 App object.
// This is useful because the V2 App object has more space information at the moment.
func convertToV2Apps(client *cfclient.Client, apps []App) []cfclient.App {
	v2Apps := []cfclient.App{}
	for _, app := range apps {
		v2App, err := client.GetAppByGuid(app.GUID)
		if err != nil {
			log.Fatalf("Unable to convert v3 app to v2 app. App Guid %s", app.GUID)
		}
		v2Apps = append(v2Apps, v2App)
	}
	return v2Apps
}

func filterForNewlyUpdatedBuildpacks(buildpacks []cfclient.Buildpack, store notifyStore) []cfclient.Buildpack {
	filteredBuildpacks := []cfclient.Buildpack{}
	storedBuildpacks := store.GetBuildpacks()
	// Go through the passed in buildpacks
	// Check if current buildpack.guid matches a guid in storeBuildpacks
	// 1) If so, compare the buildpack.Meta.UpdatedAt with the storeBuildpack.LastUpdatedAt
	// 1a)   If buildpack.Meta.UpdatedAt (updated recently) > storeBuildpack.LastUpdatedAt,
	//       then add to filteredBuildpacks and updated database
	// 1b)   Else, continue
	// 2) If not, add to filteredBuildpacks and updated database
	// for buildpacks return buildpack.guid in stored.

	for _, buildpack := range buildpacks {
		storedBuildpack, found := storedBuildpacks[buildpack.Guid]
		if !found {
			filteredBuildpacks = append(filteredBuildpacks, buildpack)
			store.SaveBuildpack(&buildpackRecord{Guid: buildpack.Guid, LastUpdatedAt: buildpack.UpdatedAt})
		} else {
			buildpackUpdatedAt, err := time.Parse(time.RFC3339, buildpack.UpdatedAt)
			if err != nil {
				log.Fatalf("Unable to parse buildpack updatedAt time. Buildpack GUID %s Error %s",
					buildpack.Guid, err)
			}
			storedBuildpackUpdatedAt, err := time.Parse(time.RFC3339, storedBuildpack.LastUpdatedAt)
			if err != nil {
				log.Fatalf("Unable to parse stored buildpack LastUpdatedAt time. Buildpack GUID %s Error %s",
					storedBuildpack.Guid, err)
			}

			if buildpackUpdatedAt.After(storedBuildpackUpdatedAt) {
				filteredBuildpacks = append(filteredBuildpacks, buildpack)
				storedBuildpack.LastUpdatedAt = buildpack.UpdatedAt
				store.SaveBuildpack(&storedBuildpack)
			} else {
				log.Printf("Supported Buildpack %s has not been updated\n", buildpack.Name)
				continue
			}
		}

	}

	return filteredBuildpacks
}

func getAppsAndBuildpacks(client *cfclient.Client, store notifyStore) ([]App, map[string]cfclient.Buildpack) {
	apps, err := ListApps(client)
	if err != nil {
		log.Fatalf("Unable to get apps. Error: %s", err.Error())
	}
	// Get all the buildpacks from our CF deployment via CF_API.
	buildpackList, err := client.ListBuildpacks()
	if err != nil {
		log.Fatalf("Unable to get buildpacks. Error: %s", err)
	}
	filteredBuildpackList := filterForNewlyUpdatedBuildpacks(buildpackList, store)

	// Create a map with the key being the buildpack name for quick comparison later on.
	buildpacks := make(map[string]cfclient.Buildpack)
	for _, buildpack := range filteredBuildpackList {
		buildpacks[buildpack.Name] = buildpack
	}
	return apps, buildpacks
}

// isDropletUsingSupportedBuildpack checks the buildpacks the droplet is using and comparing to see if one of them
// is a provided system buildpack.
func isDropletUsingSupportedBuildpack(droplet Droplet, buildpacks map[string]cfclient.Buildpack) (bool, *cfclient.Buildpack) {
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
func isDropletUsingOutdatedBuildpack(client *cfclient.Client, droplet Droplet, buildpack *cfclient.Buildpack) bool {
	timeOfLastAppRestage, err := time.Parse(time.RFC3339, droplet.CreatedAt)
	if err != nil {
		log.Fatalf("Unable to parse last restage time. Droplet GUID %s Error %s",
			droplet.GUID, err)
	}
	timeOfLastBuildpackUpdate, err := time.Parse(time.RFC3339, buildpack.UpdatedAt)
	if err != nil {
		log.Fatalf("Unable to parse last buildpack update time. Buildpack %s Buildpack GUID %s Error %s",
			buildpack.Name, buildpack.Guid, err)
	}
	return timeOfLastBuildpackUpdate.After(timeOfLastAppRestage)
}

type cfSpaceCache struct {
	spaceUsers map[string]map[string]cfclient.SpaceRole
}

func createCFSpaceCache() *cfSpaceCache {
	return &cfSpaceCache{
		spaceUsers: make(map[string]map[string]cfclient.SpaceRole),
	}
}

func filterForValidEmailUsernames(users []cfclient.SpaceRole, app cfclient.App) []cfclient.SpaceRole {
	var filteredUsers []cfclient.SpaceRole
	for _, user := range users {
		if _, err := mail.ParseAddress(user.Username); err == nil {
			filteredUsers = append(filteredUsers, user)
		} else {
			log.Printf("Dropping notification to user %s about app %s in space %s because "+
				"invalid e-mail address\n", user.Username, app.Name, app.SpaceGuid)
		}
	}
	return filteredUsers
}

func (c *cfSpaceCache) getOwnersInAppSpace(app cfclient.App, client *cfclient.Client) map[string]cfclient.SpaceRole {
	var ok bool
	var ownersWithSpaceRoles map[string]cfclient.SpaceRole
	if ownersWithSpaceRoles, ok = c.spaceUsers[app.SpaceGuid]; ok {
		return ownersWithSpaceRoles
	}
	space, err := app.Space()
	if err != nil {
		log.Fatalf("Unable to get space of app %s. Error: %s", app.Name, err.Error())
	}
	spaceRoles, err := space.Roles()
	if err != nil {
		log.Fatalf("Unable to get roles for all users in space %s. Error: %s", space.Name, err.Error())
	}
	spaceRoles = filterForValidEmailUsernames(spaceRoles, app)
	ownersWithSpaceRoles = filterForUsersWithRoles(spaceRoles, getAppOwnerRoles())

	c.spaceUsers[app.SpaceGuid] = ownersWithSpaceRoles

	return ownersWithSpaceRoles
}

// Returns a map of space roles we consider to be an owner.
// We return a map for quick look-ups and comparisons.
func getAppOwnerRoles() map[string]bool {
	return map[string]bool{
		"space_manager":   true,
		"space_developer": true,
	}
}

func filterForUsersWithRoles(spaceUsers []cfclient.SpaceRole, filteredRoles map[string]bool) map[string]cfclient.SpaceRole {
	filteredSpaceUsers := make(map[string]cfclient.SpaceRole)
	for _, spaceUser := range spaceUsers {
		if spaceUserHasRoles(spaceUser, filteredRoles) {
			filteredSpaceUsers[spaceUser.Guid] = spaceUser
		}
	}
	return filteredSpaceUsers
}

func findOwnersOfApps(apps []cfclient.App, client *cfclient.Client) map[string][]cfclient.App {
	// Mapping of users to the apps.
	owners := make(map[string][]cfclient.App)
	spaceCache := createCFSpaceCache()
	for _, app := range apps {
		// Get the space
		ownersWithSpaceRoles := spaceCache.getOwnersInAppSpace(app, client)
		for _, ownerWithSpaceRoles := range ownersWithSpaceRoles {
			owners[ownerWithSpaceRoles.Username] = append(owners[ownerWithSpaceRoles.Username], app)
		}
	}
	return owners
}

// getCurrentDropletForApp will try to query the current droplet.
// A running app will have 1 droplet associated with it.
// If it doesn't have 1, it's not running. There should be no case when it's more
// than 1 but if so, we need to do further investigation to handle it.
func getCurrentDropletForApp(app App, client *cfclient.Client) (Droplet, bool) {
	droplets, err := app.GetDropletsByQuery(client, url.Values{"current": []string{"true"}})
	if err != nil {
		// Log and continue if droplet not found
		log.Printf("Unable to get droplet for app. App %s App GUID %s Error %s",
			app.Name, app.GUID, err)
		return Droplet{}, false
	}
	if len(droplets) != 1 {
		// We should only have 1.
		return Droplet{}, false
	}
	return droplets[0], true
}

func findOutdatedApps(client *cfclient.Client, apps []App, buildpacks map[string]cfclient.Buildpack) (outdatedApps []App) {
	for _, app := range apps {
		if app.State != "STARTED" {
			log.Printf("App %s guid %s not in STARTED state\n", app.Name, app.GUID)
			continue
		}
		droplet, foundDroplet := getCurrentDropletForApp(app, client)
		if !foundDroplet {
			log.Printf("Unable to find current droplet for app %s guid %s. Safely skipping.\n", app.Name, app.GUID)
			continue
		}
		yes, buildpack := isDropletUsingSupportedBuildpack(droplet, buildpacks)
		if !yes {
			log.Printf("App %s guid %s not using supported buildpack\n", app.Name, app.GUID)
			continue
		}
		// If the app is using a supported buildpack, check if app is using an outdated buildpack.
		if appIsOutdated := isDropletUsingOutdatedBuildpack(client, droplet, buildpack); !appIsOutdated {
			log.Printf("App %s Guid %s | Buildpack %s not outdated\n", app.Name, app.GUID, buildpack.Name)
			continue
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

func sendNotifyEmailToUsers(users map[string][]cfclient.App, templates *Templates, mailer Mailer, dryRun bool) {
	for user, apps := range users {
		// Create buffer
		body := new(bytes.Buffer)
		// Determine whether the user has one application or more than one.
		isMultipleApp := false
		if len(apps) > 1 {
			isMultipleApp = true
		}
		// Fill buffer with completed e-mail
		templates.getNotifyEmail(body, notifyEmail{user, apps, isMultipleApp})
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
