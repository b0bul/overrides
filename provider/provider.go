package provider

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/zclconf/go-cty/cty"
)

const providerBackupFileExtension string = ".overrides"
const hclFileExtension string = ".hcl"
const tfFileExtension string = ".tf"

func check(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

/*
The structure of a providers.tf file can vary wildly depend on what fields of the provider block are present
There for in the overrides.tf file a simpler provider block replaces the more complex original one discarding
certain fields, like tags and allowed_account_ids etc
*/
type ProviderConfig struct {
	Terraform *TerrafromConfigBody    `hcl:"terraform,block"` // discarded when writing overrides.tf
	Providers []AwsProviderConfigBody `hcl:"provider,block"`
}

type Tag struct {
	//Tags interface{} `hcl:"tags,optional"` // discarded when writing overrides.tf
	Tags map[string]string `hcl:"tags,optional"` // discarded when writing overrides.tf

}

type AssumedRole struct {
	Tags interface{} `hcl:"role_arn,optional"` // discarded when writing overrides.tf
}

type TerrafromConfigBody struct {
	RequiredProvider hcl.Body `hcl:",remain"` // discarded when writing overrides.tf - defer decoding to the required_providers block
}

// Properties here are optional, due to "template" and "archive" providers having more often than not, no arguments {}
type AwsProviderConfigBody struct {
	Type              string       `hcl:"type,label"`
	Region            string       `hcl:"region,optional"`
	Alias             string       `hcl:"alias,optional"`
	AllowedAccountIds []string     `hcl:"allowed_account_ids,optional"` // discarded when writing overrides.tf
	DefaultTags       *Tag         `hcl:"default_tags,block"`           // *Block are set to nil pointer when empty this is how they're ignored
	AssumeRole        *AssumedRole `hcl:"assume_role,block"`            // *Block are set to nil pointer when empty this is how they're ignored
}

type OverrideConfig struct {
	Override []OverrideConfigBody `hcl:"override,block"`
}

type OverrideConfigBody struct {
	Alias   string `hcl:"alias,label"`
	Profile string `hcl:"profile,attr"`
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

// providers file can be provider.tf or providers.tf
func setProvidersFile(verbose bool) (providers string, backup string) {
	supportProviderFiles := []string{"providers.tf", "provider.tf"}
	for _, providerFile := range supportProviderFiles {

		potentialProviderBackup := providerFile + providerBackupFileExtension

		_, err := os.Stat(providerFile)

		if err != nil {
			if verbose {
				log.Println("no provider file named:", providerFile)
			}
		} else {
			providers = providerFile
		}

		_, err = os.Stat(potentialProviderBackup)

		if err != nil {
			if verbose {
				log.Println("no backup file named:", potentialProviderBackup)
			} // if it just doesn't exist it needs to be created later
		} else {
			backup = potentialProviderBackup
		}

		if providers == "" && backup == "" {
			log.Fatalln("no provider or backup file detect")
		}

		if backup == "" {
			backup = providers + providerBackupFileExtension
		}

		if providers == "" {
			providers = strings.TrimSuffix(backup, filepath.Ext(backup))
		}
	}
	return providers, backup
}

func cleanTemp(verbose bool, tmp string) {
	filesToRemove, err := os.ReadDir(tmp)

	if err != nil {
		if verbose {
			log.Println("error reading temp dir", tmp)
			log.Println("cleaning temp", tmp)
		}
		check(err)
	}

	if verbose {
		log.Println("successfully read temp dir", tmp)
	}

	for _, path := range filesToRemove {
		if !path.IsDir() {
			if strings.Contains(path.Name(), hclFileExtension) {
				filename := filepath.Join(tmp, path.Name())
				err = os.Remove(filename)
				if err != nil {
					if verbose {
						log.Println("error cleaning temp dir", filename)
					}
					check(err)
				}
			}
		}
	}
}

func parseLocals(verbose bool, tmp string) []LocalsBlock {
	cleanTemp(verbose, tmp)

	if verbose {
		log.Println("parsing locals")
	}

	allLocalsBlocksFound := []LocalsBlock{}

	terraformFiles, err := os.ReadDir(".")
	if err != nil {
		if verbose {
			log.Println("error reading the current directory")
		}
		check(err)
	}

	for _, f := range terraformFiles {
		if !f.IsDir() {
			if strings.HasSuffix(f.Name(), tfFileExtension) && !strings.Contains(f.Name(), "override") {
				hcl_fn := f.Name() + hclFileExtension
				createHclCopy(verbose, hcl_fn, f.Name(), tmp)
			}
		}
	}

	// files removed, so tmp is empty
	hclFiles, err := os.ReadDir(tmp)
	if err != nil {
		if verbose {
			log.Println("error reading tmp", tmp)
		}
		check(err)
	}

	for _, f := range hclFiles {
		var local LocalsBlock
		if strings.HasSuffix(f.Name(), hclFileExtension) {
			hcl_fn := filepath.Join(tmp, f.Name())
			hclFile, err := os.Open(hcl_fn)

			if err != nil {
				if verbose {
					log.Println("error opening", hcl_fn)
				}
				check(err)
			}

			defer hclFile.Close()

			err = hclsimple.DecodeFile(hclFile.Name(), nil, &local)

			if err != nil {
				if verbose {
					log.Println("error performing hcl decode on", hclFile.Name())
				}
				check(err)
			}

			allLocalsBlocksFound = append(allLocalsBlocksFound, local)
		}
	}

	return allLocalsBlocksFound
}

// copy .tf files as .hcl file to tmp
func createHclCopy(verbose bool, hclFile string, tfFile string, tmp string) string {
	if verbose {
		log.Println("create hcl copy", tfFile)
	}

	tfConfig, err := os.Open(tfFile)

	if err != nil {
		if verbose {
			log.Println("error reading terraform file:", tfFile)
		}
		check(err)
	}

	fullPath := filepath.Join(tmp, hclFile)

	hclConfig, err := os.OpenFile(fullPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY|os.O_TRUNC, 0644)

	if err != nil {
		if verbose {
			log.Println("error reading hcl file:", fullPath)
		}
		check(err)
	}

	defer hclConfig.Close()
	defer tfConfig.Close()

	_, err = io.Copy(hclConfig, tfConfig)

	if err != nil {
		if verbose {
			log.Printf("error copying %s to %s", tfConfig.Name(), hclConfig.Name())
		}
		check(err)
	}

	return hclConfig.Name()
}

// read provider files and interpolate locals
func ParseProviderFile(verboseLogging bool, tmpProvider string, backedupProviderFile string, tmp string) ProviderConfig {

	if verboseLogging {
		log.Println("parsing provider file")
	}

	locals := parseLocals(verboseLogging, tmp)

	hclConfigName := createHclCopy(verboseLogging, tmpProvider, backedupProviderFile, tmp)

	// fmt.Println(hclConfigName)

	/* hcl is a strucutred configuration language not a data structure serialization language like JSON,YAML or TOML
		   hcl is always decoded using an application-defined schema not via tokenization.
		   here local.management_account_id is defined by creating a local map[string]string and adding value
	       management_account_id = 12345678910 etc
		   this is required to read the providers.tf file where variables are declared in main.tf or some other file
		   the values are discarded for the read only overrides.tf file be required to read
	*/

	var defaultTags map[string]cty.Value
	var repositoryName string

	for _, block := range locals {

		for _, values := range block.Locals {
			if values.DefaultTags != nil {
				defaultTags = map[string]cty.Value{
					"Repository": cty.StringVal(values.DefaultTags["Repository"]),
				}
			}
			if values.RepositoryName != "" {
				repositoryName = values.RepositoryName
			}
		}
	}

	// if not local.default_tags set discard, can't set empty cty.MapVal()
	if len(defaultTags) == 0 {
		defaultTags = map[string]cty.Value{
			"discarded": cty.StringVal("discarded"),
		}
	}

	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{"local": cty.ObjectVal(map[string]cty.Value{
			"default_tags":          cty.MapVal(defaultTags),
			"repository_name":       cty.StringVal(repositoryName),
			"management_account_id": cty.StringVal("12345678910"),
			"account_number":        cty.StringVal("12345678910"),
			"build_account_id":      cty.StringVal("12345678910"),
			"transit_account_id":    cty.StringVal("12345678910"),
			"hmpo_account_id":       cty.StringVal("12345678910"),
			"logs_account_id":       cty.StringVal("12345678910"),
		},
		),
		},
	}

	var config ProviderConfig
	err := hclsimple.DecodeFile(hclConfigName, ctx, &config)
	defer os.Remove(hclConfigName)
	check(err)
	return config
}

func ParseOverrideConfig(mappingFile string) OverrideConfig {

	hclConfig, err := os.OpenFile(mappingFile, os.O_RDONLY, 0644)
	check(err)

	defer hclConfig.Close()

	var config OverrideConfig

	err = hclsimple.DecodeFile(hclConfig.Name(), nil, &config)
	check(err)

	return config
}

// take non-deterministic shape of any default tags and convert to appropriately from hcl to go for writing to file
// determine which locals keys need to be resolved to local.values
func ExtractDefaultTags(verbose bool, config []AwsProviderConfigBody) map[string]string {

	if verbose {
		log.Println("extracting default tags")
	}

	tagMapSorting := make(map[string][]string)

	for _, provider := range config {
		var groupTags []string
		if provider.DefaultTags != nil {
			for key, value := range provider.DefaultTags.Tags {
				groupTags = append(groupTags, fmt.Sprintf("   %v   =   \"%v\"\n", key, value))
				if provider.Alias == "" {
					tagMapSorting["unaliased"] = groupTags
				} else {
					tagMapSorting[provider.Alias] = groupTags
				}
			}
		}
	}

	var tagString string

	tagMapComplete := make(map[string]string)

	if len(tagMapSorting) > 0 {
		for key, values := range tagMapSorting {
			tagString = ""
			for _, tag := range values {
				tagString += fmt.Sprintf("%v", tag)
			}
			tagMapComplete[key] = tagString
		}
		return tagMapComplete
	}
	return tagMapComplete
}

// Remove the .override file and restore the current working directory to working order
func RestoreProvider(verboseLogging bool, overrideFile string) (string, string) {
	providersFile, backedupProviderFile := setProvidersFile(verboseLogging)

	_, err := os.Stat(overrideFile)

	if err != nil {
		if verboseLogging {
			log.Printf("no %s file detected, skipping cleanup", overrideFile)
		}
	} else {
		if verboseLogging {
			log.Println("cleaning up previous:", overrideFile)
		}
		err = os.Remove(overrideFile)
		check(err)
	}

	_, err = os.Stat(backedupProviderFile)

	if err != nil {
		if verboseLogging {
			log.Printf("no %s backup file detected, skipping restore", backedupProviderFile)
		}
	} else {
		if verboseLogging {
			log.Printf("restoring providers file %s to %s", backedupProviderFile, providersFile)
		}
		err = os.Rename(backedupProviderFile, providersFile)
		if err != nil {
			check(err)
		}
	}
	return providersFile, backedupProviderFile
}

// Backup the providers.tf file and create and overrides.tf file that will work in its place
func BackupProvider(verboseLogging bool, overrideFile string, providerFile string, backedupProviderFile string) {

	if verboseLogging {
		log.Printf("backing up provider file %s as %s", providerFile, backedupProviderFile)
	}

	err := os.Rename(providerFile, backedupProviderFile)

	if err != nil {
		if verboseLogging {
			log.Printf("error backing up provider file %s as %s", providerFile, backedupProviderFile)
		}
		check(err)
	}

	if verboseLogging {
		log.Println("creating override file", overrideFile)
	}

	file, err := os.OpenFile(overrideFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY|os.O_TRUNC, 0644)

	if err != nil {
		if verboseLogging {
			log.Println("error creating override file", overrideFile)
		}
		check(err)
	}

	defer file.Close()
}
