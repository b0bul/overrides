package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
)

type ConfigOptions struct {
	DefaultConfigFile                   string `json:"-"` // not configureable
	DefaultIntermediateProviderFile     string `json:"-"`
	DefaultMappingFile                  string `json:"-"`
	DefaultTfPluginCache                string `json:"-"`
	DefaultOverrideProviderFile         string `json:"overrides_provider_file,omitempty"`
	DefaultBatch                        int    `json:"chunks,omitempty"`
	DefaultWorkers                      int    `json:"threads,omitempty"`
	DefaultVerbose                      bool   `json:"verbose"`
	DefaultRefresh                      bool   `json:"refresh"`
	DefaultTmpDir                       string `json:"tmp_dir,omitempty"`
	DefaultUseCredentialsFile           bool   `json:"use_credentials_file"`
	DefaultAwsSsoCacheDir               string `json:"aws_sso_cache_dir"`
	DefaultAwsCredentialsFile           string `json:"aws_credentials_file"`
	DefaultAwsSsoConfigFile             string `json:"aws_sso_profiles_config"`
	DefaultAwsProfileName               string `json:"aws_sso_profile_name"`
	DefaultAwsProfileAccountId          string `json:"aws_sso_profile_account_id"`
	DefaultAwsProfileRole               string `json:"aws_sso_profile_role"`
	DefaultAwsRegion                    string `json:"aws_region"`
	DefaultAwsSsoStartUrl               string `json:"aws_sso_start_url"`
	DefaultResetAwsSsoConfigFile        bool   `json:"reset_to_default_aws_sso_config_file"`
	DefaultAwsSsoConfigFileUnderControl bool   `json:"-"`
}

func newDefaultConfig() *ConfigOptions {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Println("couldn't get home dir from environment")
	}
	configFullFile := filepath.Join(homeDir, ".override")
	awsSsoCacheDir := filepath.Join(homeDir, ".aws", "sso", "cache")
	awsSsoCredentialsFile := filepath.Join(homeDir, ".aws", "credentials")
	awsSsoConfigFile := filepath.Join(homeDir, ".aws", "config")
	tmpDir := os.TempDir()
	intermediateProviderFile := "config.hcl"
	mappingFile := "mappings.hcl"
	terraformCacheDir := ".terraform"
	overrideProviderFile := "overrides.tf"
	awsSsoProfileName := "<org>-<account>-<env>-<role>"
	awsSssoProfileAccountId := "12345678910" // make real
	awsSsoProfileRole := "CodeContributor"
	awsRegion := "eu-west-2"
	awsSsoStartUrl := "https://<org>.awsapps.com/start"
	resetAwsSsoConfigFile := false
	useCredentialsFile := false
	if runtime.GOOS == "windows" {
		useCredentialsFile = true
	}

	return &ConfigOptions{
		DefaultConfigFile:                   configFullFile,
		DefaultMappingFile:                  mappingFile,
		DefaultIntermediateProviderFile:     intermediateProviderFile,
		DefaultTfPluginCache:                terraformCacheDir,
		DefaultOverrideProviderFile:         overrideProviderFile,
		DefaultBatch:                        4,
		DefaultWorkers:                      12,
		DefaultVerbose:                      false,
		DefaultRefresh:                      false,
		DefaultTmpDir:                       tmpDir,
		DefaultUseCredentialsFile:           useCredentialsFile,
		DefaultAwsSsoCacheDir:               awsSsoCacheDir,
		DefaultAwsSsoConfigFile:             awsSsoConfigFile,
		DefaultAwsCredentialsFile:           awsSsoCredentialsFile,
		DefaultAwsProfileName:               awsSsoProfileName,
		DefaultAwsProfileAccountId:          awsSssoProfileAccountId,
		DefaultAwsProfileRole:               awsSsoProfileRole,
		DefaultAwsRegion:                    awsRegion,
		DefaultAwsSsoStartUrl:               awsSsoStartUrl,
		DefaultResetAwsSsoConfigFile:        resetAwsSsoConfigFile,
		DefaultAwsSsoConfigFileUnderControl: true,
	}
}

var appConfig *ConfigOptions

// var app Override

func init() {
	// override config
	appConfig = newDefaultConfig()
	appConfig.readConfigFromDisk()
	// aws config
	appConfig.DefaultAwsSsoConfigFileUnderControl = appConfig.preReadAwsSsoConfig()
	appConfig.takeOverAwsSsoConfig()
}

func (c ConfigOptions) rewriteAwsSsoConfig(file *os.File) {
	template := `# overrides managed
[profile %v]
sso_start_url=%v
sso_region=%v
sso_account_id=%v
sso_role_name=%v
region=%v
output=json
# overrides managed
[profile %v]
sso_start_url=%v
sso_region=%v
sso_account_id=%v
sso_role_name=%v
region=%v
output=json`

	profile := fmt.Sprintf(template,
		c.DefaultAwsProfileName,
		c.DefaultAwsSsoStartUrl,
		c.DefaultAwsRegion,
		c.DefaultAwsProfileAccountId,
		c.DefaultAwsProfileRole,
		c.DefaultAwsRegion,
		"daas",
		c.DefaultAwsSsoStartUrl,
		c.DefaultAwsRegion,
		"644377453469",
		"WorkspaceUsers",
		c.DefaultAwsRegion,
	)
	_, err := file.Write([]byte(profile))

	if err != nil {
		log.Fatalf("error writing default profile %v\nerror: %v\n", profile, err)
	}
}

func (c ConfigOptions) ResetAwsSsoConfig(r bool) {
	c.DefaultResetAwsSsoConfigFile = r
	f := c.getAwsSsoConfigHanlde()

	defer f.Close()

	c.rewriteAwsSsoConfig(f)
}

// determine if config file is under the applications control
func (c ConfigOptions) preReadAwsSsoConfig() bool {
	fileInfo, err := os.Stat(c.DefaultAwsSsoConfigFile)

	if os.IsNotExist(err) {
		return false
	}

	if fileInfo.Size() == 0 {
		return false
	}

	file, err := os.Open(c.DefaultAwsSsoConfigFile)
	if err != nil {
		log.Fatalf("error opening aws config file %v with error: %v", c.DefaultAwsSsoConfigFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Scan()
	fileLine := scanner.Text()

	if err := scanner.Err(); err != nil {
		log.Fatalln(err)
	}

	if fileLine != "# overrides managed" {
		return false
	}

	return true
}

func (c ConfigOptions) getAwsSsoConfigHanlde() *os.File {
	var f *os.File
	_, err := os.Stat(c.DefaultAwsSsoConfigFile)

	if os.IsNotExist(err) {
		// safe to assume config not under the applications control
		f, err = os.Create(c.DefaultAwsSsoConfigFile)
		if err != nil {
			log.Fatalf("error creating config file %v with error %v", c.DefaultAwsSsoConfigFile, err)
		}
		return f
	}

	// if under control not being reset append
	// if under control but being reset truncate
	// if not under control             truncase
	if !c.DefaultResetAwsSsoConfigFile && c.DefaultAwsSsoConfigFileUnderControl {
		f, err = os.OpenFile(c.DefaultAwsSsoConfigFile, os.O_APPEND|os.O_WRONLY, os.ModePerm)
		if err != nil {
			log.Fatalf("error opening config file %v with error %v", c.DefaultAwsSsoConfigFile, err)
		}
		return f
	}

	f, err = os.OpenFile(c.DefaultAwsSsoConfigFile, os.O_TRUNC|os.O_WRONLY, os.ModePerm)
	if err != nil {
		log.Fatalf("error opening config file %v with error %v", c.DefaultAwsSsoConfigFile, err)
	}
	return f

}

// take over is to flush your current config and bring it under the control of the override application
func (c ConfigOptions) takeOverAwsSsoConfig() {
	f := c.getAwsSsoConfigHanlde()
	ssoConfigStats, err := os.Stat(c.DefaultAwsSsoConfigFile)
	if err != nil {
		log.Fatalln("error stating config file", c.DefaultAwsSsoConfigFile)
	}

	if ssoConfigStats.Size() == 0 || c.DefaultResetAwsSsoConfigFile || !c.DefaultAwsSsoConfigFileUnderControl {
		if c.DefaultVerbose {
			log.Println("aws config file empty, populating with default profile")
		}
		// opened with the wrong flag
		c.rewriteAwsSsoConfig(f)
	}
	defer f.Close()
}

func GetConfig() *ConfigOptions {
	return appConfig
}

func (c *ConfigOptions) readConfigFromDisk() {
	var configFromDisk ConfigOptions

	// might not be set but it's options supersede the defaults from the app
	// so you have to read it first
	d, err := os.ReadFile(c.DefaultConfigFile)
	if err != nil {
		// write defaults
		c.writeConfigFileToDisk()
		d, err = os.ReadFile(c.DefaultConfigFile)

		if err != nil {
			fmt.Println("error reading config file", err)
		}
	}

	if err = json.Unmarshal(d, &configFromDisk); err != nil {
		fmt.Println(err)
	}

	// config reset just removes existing ~/.override file and app will rebuild it

	if configFromDisk.DefaultTmpDir != c.DefaultTmpDir {
		c.DefaultTmpDir = configFromDisk.DefaultTmpDir
	}

	if configFromDisk.DefaultOverrideProviderFile != c.DefaultOverrideProviderFile {
		c.DefaultOverrideProviderFile = configFromDisk.DefaultOverrideProviderFile
	}

	if configFromDisk.DefaultBatch != c.DefaultBatch {
		c.DefaultBatch = configFromDisk.DefaultBatch
	}

	if configFromDisk.DefaultWorkers != c.DefaultWorkers {
		c.DefaultWorkers = configFromDisk.DefaultWorkers
	}

	if configFromDisk.DefaultVerbose != c.DefaultVerbose {
		c.DefaultVerbose = configFromDisk.DefaultVerbose
	}

	if configFromDisk.DefaultRefresh != c.DefaultRefresh {
		c.DefaultRefresh = configFromDisk.DefaultRefresh
	}

	if configFromDisk.DefaultTmpDir != c.DefaultTmpDir {
		c.DefaultTmpDir = configFromDisk.DefaultTmpDir
	}

	if configFromDisk.DefaultAwsCredentialsFile != c.DefaultAwsCredentialsFile {
		c.DefaultAwsCredentialsFile = configFromDisk.DefaultAwsCredentialsFile
	}

	if configFromDisk.DefaultUseCredentialsFile != c.DefaultUseCredentialsFile {
		c.DefaultUseCredentialsFile = configFromDisk.DefaultUseCredentialsFile
	}

	if configFromDisk.DefaultAwsSsoCacheDir != c.DefaultAwsSsoCacheDir {
		c.DefaultAwsSsoCacheDir = configFromDisk.DefaultAwsSsoCacheDir
	}

	if configFromDisk.DefaultAwsSsoConfigFile != c.DefaultAwsSsoConfigFile {
		c.DefaultAwsSsoConfigFile = configFromDisk.DefaultAwsSsoConfigFile
	}

	if configFromDisk.DefaultAwsSsoStartUrl != c.DefaultAwsSsoStartUrl {
		c.DefaultAwsSsoStartUrl = configFromDisk.DefaultAwsSsoStartUrl
	}

	if configFromDisk.DefaultAwsRegion != c.DefaultAwsRegion {
		c.DefaultAwsRegion = configFromDisk.DefaultAwsRegion
	}

	if configFromDisk.DefaultAwsProfileName != c.DefaultAwsProfileName {
		c.DefaultAwsProfileName = configFromDisk.DefaultAwsProfileName
	}

	if configFromDisk.DefaultAwsProfileAccountId != c.DefaultAwsProfileAccountId {
		c.DefaultAwsProfileAccountId = configFromDisk.DefaultAwsProfileAccountId
	}

	if configFromDisk.DefaultAwsProfileRole != c.DefaultAwsProfileRole {
		c.DefaultAwsProfileRole = configFromDisk.DefaultAwsProfileRole
	}

	if configFromDisk.DefaultResetAwsSsoConfigFile != c.DefaultResetAwsSsoConfigFile {
		c.DefaultResetAwsSsoConfigFile = configFromDisk.DefaultResetAwsSsoConfigFile
	}

}

func (c ConfigOptions) writeConfigFileToDisk() {
	d, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		fmt.Println("error mashaling config json", err)
	}
	if err = os.WriteFile(c.DefaultConfigFile, d, 0644); err != nil {
		fmt.Println("error writing config file", err)
	}
}

func validateField(k string, v reflect.Value) (reflect.Value, error) {
	var field reflect.Value

	switch {
	case k == "tmp_dir":
		field = v.FieldByName("DefaultTmpDir")
	case k == "chunks":
		field = v.FieldByName("DefaultBatch")
	case k == "threads":
		field = v.FieldByName("DefaultWorkers")
	case k == "verbose":
		field = v.FieldByName("DefaultVerbose")
	case k == "refresh":
		field = v.FieldByName("DefaultRefresh")
	case k == "use_credentials_file":
		field = v.FieldByName("DefaultUseCredentialsFile")
	case k == "aws_sso_cache_dir":
		field = v.FieldByName("DefaultAwsSsoCacheDir")
	case k == "aws_credentails_file":
		field = v.FieldByName("DefaultAwsCredentialsFile")
	case k == "aws_sso_profile_config":
		field = v.FieldByName("DefaultAwsSsoConfigFile")
	case k == "overrides_provider_file":
		field = v.FieldByName("DefaultOverrideProviderFile")
	case k == "aws_region":
		field = v.FieldByName("DefaultAwsRegion")
	case k == "aws_sso_profile_name":
		field = v.FieldByName("DefaultAwsProfileName")
	case k == "aws_sso_profile_account_id":
		field = v.FieldByName("DefaultAwsProfileAccountId")
	case k == "aws_sso_profile_role":
		field = v.FieldByName("DefaultAwsProfileRole")
	case k == "aws_sso_start_url":
		field = v.FieldByName("DefaultAwsSsoConfigFile")
	case k == "reset_to_default_aws_sso_config_file":
		field = v.FieldByName("DefaultResetAwsSsoConfigFile")
	default:
		return field, errors.New("error finding in config item")
	}
	return field, nil
}

func setField(c *ConfigOptions, k string, nv string) error {
	v := reflect.ValueOf(c).Elem()
	field, err := validateField(k, v)

	if err != nil {
		fmt.Println("error validating field", err, field)
	}

	if !field.IsValid() {
		return fmt.Errorf("no such field: %s in config", k)
	}

	if !field.CanSet() {
		return fmt.Errorf("connot set field: %s", k)
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(nv)
	case reflect.Int:
		i, err := strconv.Atoi(nv)
		if err != nil {
			return errors.New("can't convert string to int")
		}
		field.SetInt(int64(i))
	case reflect.Bool:
		boolValue, err := strconv.ParseBool(nv)
		if err != nil {
			return errors.New("invalid boolean value")
		}
		field.SetBool(boolValue)
	default:
		return fmt.Errorf("unsupported field type: %s", k)
	}
	return nil
}

func (c ConfigOptions) Set(key string, value string) {
	var currentConfig ConfigOptions

	// might not be set but it's options supersede the defaults from the app
	// so you have to read it first
	diskConfig, err := os.ReadFile(c.DefaultConfigFile)

	if err != nil {
		fmt.Println("error reading config file", err)
	}

	if err = json.Unmarshal(diskConfig, &currentConfig); err != nil {
		fmt.Println("error unmarshaling json", err)
	}

	err = setField(&currentConfig, key, value)

	if err != nil {
		fmt.Println("error setting config file", err)
	}

	newConfig, err := json.MarshalIndent(currentConfig, "", " ")
	if err != nil {
		fmt.Println("error mashaling config json", err)
	}
	if err = os.WriteFile(c.DefaultConfigFile, newConfig, 0644); err != nil {
		fmt.Println("error writing config file", err)
	}
}

func (c ConfigOptions) Show() {
	b, err := json.MarshalIndent(c, "", " ")

	if err != nil {
		fmt.Println("error mashaling json", err)
	}

	fmt.Println(string(b))
}
