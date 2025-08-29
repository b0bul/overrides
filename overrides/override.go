package overrides

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/b0bul/override/aws"
	co "github.com/b0bul/override/config"
	pd "github.com/b0bul/override/provider"
	"github.com/hashicorp/hcl/v2"
)

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

type Override struct {
	ConfigFile               string
	MappingFile              string
	IntermediateProviderFile string
	TfPluginCacheDir         string
	OverrideProviderFile     string
	Accounts                 []aws.Account
	Batch                    int
	Verbose                  bool
	Workers                  int
	Refresh                  bool
	Alias                    string
	ConfigPath               string
	TmpDir                   string
	UseCredentialsFile       bool
	AwsSsoCacheDir           string
	AwsSsoConfigFile         string
	AwsCredentialsFile       string
	ProviderFile             string
	ProviderFileBackup       string
	AwsRegion                string
	AwsSsoStartUrl           string
	ResetAwsSsoConfigFile    bool
}

func InitializeOverrideApp(c *co.ConfigOptions) Override {
	return Override{
		Batch:                    c.DefaultBatch,
		Workers:                  c.DefaultWorkers,
		Verbose:                  c.DefaultVerbose,
		Refresh:                  c.DefaultRefresh,
		TmpDir:                   c.DefaultTmpDir,
		UseCredentialsFile:       c.DefaultUseCredentialsFile,
		AwsSsoCacheDir:           c.DefaultAwsSsoCacheDir,
		AwsSsoConfigFile:         c.DefaultAwsSsoConfigFile,
		AwsCredentialsFile:       c.DefaultAwsCredentialsFile,
		OverrideProviderFile:     c.DefaultOverrideProviderFile,
		IntermediateProviderFile: c.DefaultIntermediateProviderFile,
		MappingFile:              c.DefaultMappingFile,
		AwsRegion:                c.DefaultAwsRegion,
		AwsSsoStartUrl:           c.DefaultAwsSsoStartUrl,
		ResetAwsSsoConfigFile:    c.DefaultResetAwsSsoConfigFile,
	}
}

// add new locals here
type LocalValues struct {
	RepositoryName string            `hcl:"repository_name,optional"`
	DefaultTags    map[string]string `hcl:"default_tags,optional"`
	Options        hcl.Body          `hcl:",remain"` // discard
}

type LocalsBlock struct {
	Locals  []LocalValues `hcl:"locals,block"`
	Options hcl.Body      `hcl:",remain"` // discard
}

/*
search all .tf files and create a slice of locals for each locals block found
Ignore all locals value that are not defined in the schema of the LocalValues struct
When new locals are reuqired for templating / resolving local.something to its value
It must be added to the struct

This slice of structs is then merged together

todo
- search all .tf files in the dir
- convert them all to /tmp/dirname.filename.hcl
- parse out all locals, including multiple locals from single file
- return as expected
- map default default values setup in in-memory locals to locals from files

*/

func (app Override) WriteSsoProfiles() {

	if app.UseCredentialsFile {
		if app.Verbose {
			log.Println("credentials file being used skipping writing sso profiles, flushing ~/.aws/config")
		}
		// the chicken egg problem, on windows sso is initially required to fetch and build the credentials file
		// when credentials are being used LoadDefaultConfig loads both but uses sso first.
		// flushing this file means creds are used instead
		// I'm really just trying to get this done or I'd put more thought into it
		os.Truncate(app.AwsSsoConfigFile, 0)
		return
	}

	if app.Verbose {
		log.Println("writing sso profiles.")
	}

	cleanedPath := filepath.Clean(app.AwsSsoConfigFile)
	file, err := os.OpenFile(cleanedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		if app.Verbose {
			log.Println("error opening sso profiles", cleanedPath)
		}
	}
	check(err)
	defer file.Close()

	for _, account := range app.Accounts {
		for _, role := range account.Roles {
			profile := fmt.Sprintf("%v-%v", account.Name, role.Name)
			app.rewriteAwsSsoConfig(file,
				profile,
				app.AwsSsoStartUrl,
				app.AwsRegion,
				account.Id,
				role.Name,
			)
		}
	}
}

// returns the default profile, unless there's a special case where a provider block is unaliased, OR requires a role other than the default role
func (app Override) setProviderProfile(alias string, providerMappings pd.OverrideConfig) string {

	mappings := make(map[string]string)

	// convert struct to map
	for _, provider := range providerMappings.Override {
		// if an environment alias is passed, replace the environment with the new alias
		if app.Alias != "" {
			profile := strings.Split(provider.Profile, "-")
			profile[2] = app.Alias

			providerProfile := strings.Join(profile, "-")
			mappings[provider.Alias] = providerProfile
		} else {
			mappings[provider.Alias] = provider.Profile
		}
	}

	if _, ok := mappings[alias]; ok {
		return mappings[alias]
	}
	return mappings["default"]
}

// Write the overrides.tf file - this data structure will be replaced by a dynamic
func (app Override) WriteOverrideProvidersFileDynamic() {

	if app.Verbose {
		log.Println("writing overrides file")
	}

	config := pd.ParseProviderFile(app.Verbose, app.IntermediateProviderFile, app.ProviderFileBackup, app.TmpDir)

	defaultTags := pd.ExtractDefaultTags(app.Verbose, config.Providers)

	// temp solution until dynamic schema can be generated
	if config.Terraform != nil {
		log.Fatalln("You have a 'terraform {}' block in your providers.tf, please move ethis to version.tf")
	}

	if app.Verbose {
		log.Println("opening overrides file", app.OverrideProviderFile)
	}

	file, err := os.OpenFile(app.OverrideProviderFile, os.O_APPEND|os.O_WRONLY, 0644)

	if err != nil {
		if app.Verbose {
			log.Println("error opening overrides file", app.OverrideProviderFile)
		}
	}
	check(err)

	defer file.Close()

	// read mappings.hcl
	providerMappings := pd.ParseOverrideConfig(app.MappingFile)
	escapedcredsPath := strings.ReplaceAll(app.AwsCredentialsFile, `\`, `\\`)
	escapedSsoConfigPath := strings.ReplaceAll(app.AwsSsoConfigFile, `\`, `\\`)

	for _, provider := range config.Providers {
		switch {
		// leaving support for other providers
		// where no default tags are provided they are written empty
		case provider.Type == "aws":
			// handle default provider
			if provider.Alias == "" {
				_, err := file.Write([]byte(fmt.Sprintf("provider \"aws\" {\n	region = \"%v\"\n	profile = \"%v\"\n	shared_credentials_files = [\"%v\"]\n	shared_config_files = [\"%v\"]\n default_tags {\n   tags = {\n %v       }\n    }\n}\n",
					provider.Region,
					app.setProviderProfile("unaliased", providerMappings),
					escapedcredsPath,
					escapedSsoConfigPath,
					defaultTags["unaliased"],
				)))
				if err != nil {
					if app.Verbose {
						log.Println("error writing unaliased provider to overrides file", app.OverrideProviderFile)
					}
				}
				check(err)
				// handle all others
			} else {

				_, err := file.Write([]byte(fmt.Sprintf("provider \"aws\" {\n	region = \"%v\"\n	alias = \"%v\"\n	profile = \"%v\"\n	shared_credentials_files = [\"%v\"]\n	shared_config_files = [\"%v\"]\n default_tags {\n   tags = {\n %v       }\n    }\n}\n",
					provider.Region,
					provider.Alias,
					app.setProviderProfile(provider.Alias, providerMappings),
					escapedcredsPath,
					escapedSsoConfigPath,
					defaultTags[provider.Alias],
				)))
				if err != nil {
					if app.Verbose {
						log.Printf("error writing alias %s provider to overrides file %s", provider.Alias, app.OverrideProviderFile)
					}
				}
				check(err)
			}
		case provider.Type == "template":
			if provider.Alias == "" {
				_, err := file.Write([]byte("provider \"template\" {}\n\n"))
				if err != nil {
					if app.Verbose {
						log.Println("error writing template provider to overrides file")
					}
				}
				check(err)
			}
		case provider.Type == "archive":
			if provider.Alias == "" {
				_, err := file.Write([]byte("provider \"archive\" {}\n\n"))
				if err != nil {
					if app.Verbose {
						log.Println("error writing archive provider to overrides file")
					}
				}
				check(err)
			}
		}
	}
}

func (app Override) rewriteAwsSsoConfig(file *os.File, defaultAwsProfileName string, defaultAwsSsoStartUrl string, defaultAwsRegion string, defaultAwsProfileAccountId string, defaultAwsProfileRole string) {
	template := `# overrides managed
[profile %v]
sso_start_url=%v
sso_region=%v
sso_account_id=%v
sso_role_name=%v
region=%v
output=json
`

	profile := fmt.Sprintf(template,
		defaultAwsProfileName,
		defaultAwsSsoStartUrl,
		defaultAwsRegion,
		defaultAwsProfileAccountId,
		defaultAwsProfileRole,
		defaultAwsRegion,
	)
	_, err := file.Write([]byte(profile))

	if err != nil {
		if app.Verbose {
			log.Printf("error writing credentials file %v profile: %v", app.AwsSsoConfigFile, profile)
		}
	}
}

// Create an ini file with working aws credentials
func (app Override) WriteAwsCredentialsFile() {

	if app.Verbose {
		log.Println("writing credentials file.")
	}

	file, err := os.OpenFile(app.AwsCredentialsFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
	if err != nil {
		if app.Verbose {
			log.Println("error opening credentials file", app.AwsCredentialsFile)
		}
	}
	check(err)
	defer file.Close()

	for _, account := range app.Accounts {
		for _, role := range account.Roles {
			_, err := file.Write([]byte(fmt.Sprintf("[%v-%v]\naws_access_key_id=%v\naws_secret_access_key=%v\naws_session_token=%v\n",
				account.Name,
				role.Name,
				role.Credentials.AccessKeyId,
				role.Credentials.SecretAccessKey,
				role.Credentials.SessionToken)))
			if err != nil {
				if app.Verbose {
					log.Println("error writing credentials file", app.AwsCredentialsFile)
				}
			}
			check(err)
		}

	}
}

// Locate and filter files in the sso cache path returning the filename of the token file excluding all others
func FindTokenFile(verbose bool, cachePath string) string {
	if verbose {
		log.Println("searching for valid token")
	}

	tokenFiles, err := os.ReadDir(cachePath)
	if err != nil {
		if verbose {
			log.Println("error reading token cache", cachePath)
		}
	}
	check(err)

	for _, file := range tokenFiles {
		fileName := file.Name()
		if verbose {
			log.Printf("checking %v for token", fileName)
		}
		if strings.Contains(fileName, ".json") {
			if !strings.Contains(fileName, "botocore") {
				if verbose {
					log.Println("found token file:", fileName)
				}
				return fileName
			}
		}
	}
	return ""
}

// Marshal local credentials json file into struct extracting token
func (app Override) GetSsoToken() (aws.Token, error) {

	var buf []byte
	var token aws.Token

	tokenFile := FindTokenFile(app.Verbose, app.AwsSsoCacheDir)

	tokenFullPath := filepath.Join(app.AwsSsoCacheDir, tokenFile)

	if tokenFullPath != "" {

		if tokenFile == "" {
			return token, errors.New("token file not found in cache dir")
		}

		if app.Verbose {
			log.Println("sso token path set as:", tokenFullPath)
		}
		tokenFile, err := os.Stat(tokenFullPath)

		if os.IsNotExist(err) {
			if app.Verbose {
				log.Printf("token path %v not exists, creating:", tokenFullPath)
			}
			os.MkdirAll(app.AwsSsoCacheDir, os.ModeDir)
		}

		if tokenFile.Size() > 0 {
			fp, err := os.Open(tokenFullPath)
			if app.Verbose {
				if err != nil {
					log.Println("error opening token file", tokenFullPath)
				}
				check(err)
			}

			fileStat, err := fp.Stat()
			if app.Verbose {
				if err != nil {
					log.Println("error stating file", tokenFullPath)
				}
				check(err)
			}

			// dynamic buffer based on file size, toke file different on windows
			if fileStat.Size() > 3000 {
				if app.Verbose {
					if err != nil {
						log.Println("token size is arbitrary and larger on windows, 3k expected for saftey reasons increase as needed")
						log.Fatalf("token %v larger than 3000 bytes got %v", tokenFullPath, fileStat.Size())
					}
					check(err)
				}
			}

			buf = make([]byte, fileStat.Size())

			_, err = fp.Read(buf)
			if err != nil {
				if app.Verbose {
					log.Printf("error reason token data into buffer: %v", err)
				}
			}
			check(err)
			defer fp.Close()
		}
		if err := json.NewDecoder(bytes.NewBuffer(buf)).Decode(&token); err != nil {
			if app.Verbose {
				log.Printf("error decoding token")
			}
			check(err)
		}
		return token, nil
	}

	return token, errors.New("token file is empty, nothing to decode")
}

func (app Override) Client() *aws.Client {
	cfg, err := config.LoadDefaultConfig(context.TODO())

	if err != nil {
		if app.Verbose {
			log.Println("error loading default config", cfg.ConfigSources)
		}
		check(err)
	}

	client := sso.NewFromConfig(cfg)

	token, err := app.GetSsoToken()

	if err != nil {
		if app.Verbose {
			log.Println("error fetching token")
		}
		check(err)
	}

	return &aws.Client{Client: client, Token: token}
}

// Construct Application object consisting of account name, id and roles and credentials for each role
func (app *Override) Overrides() {
	if app.Verbose {
		log.Println("constructing main application state consisting of account, roles and credentials")
	}
	var accounts []aws.Account

	accountDataWithoutRoleData := aws.GetAccounts(app.Verbose, accounts, app.Client())
	// takes a worker pool function
	accountDataWithRoleData := aws.InterrogateRoles(accountDataWithoutRoleData, app.Client(), aws.CredentialsWorker, &app.Batch, &app.Verbose, app.UseCredentialsFile, &app.Workers)

	app.Accounts = accountDataWithRoleData
}

func (app Override) ListProfiles() {
	var accounts []aws.Account

	accountDataWithoutRoleData := aws.GetAccounts(app.Verbose, accounts, app.Client())
	// takes a worker pool function
	aws.InterrogateRoles(accountDataWithoutRoleData, app.Client(), aws.ProfilesWorker, &app.Batch, &app.Verbose, app.UseCredentialsFile, &app.Workers)
}

func (app *Override) BatchSize(bsize int) {
	app.Batch = bsize
}

func (app *Override) VerboseLogging(v bool) {
	app.Verbose = v
}

func (app *Override) ResetAwsSsoConfig(v bool) {
	app.ResetAwsSsoConfigFile = v
}

func (app *Override) SetWorkers(w int) {
	app.Workers = w
}

func (app *Override) RefreshCredentials(v bool) {
	app.Refresh = v
}

func (app *Override) SetAlias(a string) {
	app.Alias = a
}

func (app *Override) UseAwsCredentialsFile(v bool) {
	app.UseCredentialsFile = v
}
