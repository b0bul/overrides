# HCL driven provider generation.

Mostly archived tbh

Override is a cross platofrm application supported on Windows and Linux to dynamically build a terraform provider file with readOnly credentials. The project is intended for restricted cross account environments or where the IAM infrastructure is not desireable or suitable for local development. AWS Terraform providers work by assuming roles that your pipeline can assume but you likely can't. This application renders your provider files with alterative credentials that you can assume on-the-fly allowing you to locally develop. It acts as a middleware translating your providers files to a safe context before you operate terraform. This configuration is then cleaned up and never commited back to the source codebase. 

Overrides allows you to prepare your environment so that local plans can run using aws sso profiles which is the preferred method. It can also enable terraform to use aws credentials based profiles if required. The first thing this tool does is pull the read only roles for each account (or any role that you _can or want to assume for a given account_) necessary to build a safe providers file, so it assumes you're already authenticated to with a role that _can_ pull these roles. Its core purpose is to generate new terraform providers files on-the-fly to allow you to develop safely without fear of side affects to your cloud environments. You simply run the tool when you want to plan, and then restore the working state of your directory before you commit back to github. The tool's behaviour is configurable via a `~/.overrides` file that's generated for you on first run and its funcationality configurable via a `mappings.hcl` file which you commit along with your codebase which tells it how to generate `overrides.tf` providers.  

# Requirements

- awscli (availably on company portal for windows users)
- github cli 'gh' (available on company portal for windows users)
- internet connection
- ability to authenticate to aws using sso
- ability to authenitcate to github using sso
- tested with golang v1.20.2 and v1.21.6


## Linux or gitbash environment
from bash add these varaibles to your .bashrc and make sure they're loaded in your environment (ensure you remove any previous `GO` env vars). See the note about gitbash at the bottom. 

```bash
export AWS_PROFILE=<org>-master-<env>-<role>
export AWS_DEFAULT_REGION=eu-west-2
export PATH=$PATH:$HOME/go/bin
export GOPRIVATE=github.com/<org>/override
export GONOPROXY=github.com/<org>/override
```

_An extra note for windows_ Linux gitbash on windows is still windows. There a bootstrap process this tool has to figure out which OS it's on, what its config says and which credentials files are present if any. In overrides are 2 modes, sso mode and credentails mode. On windows credentials is required due to how aws is configured (using sso you'll get a 403 trying to do very basic things that just work on linux). With linux everything defaults to sso unless otherwise specified. This bootstrap has a chicken-egg problem, if your credentials are expired (which they will) how do you get new ones? Sso is used to get the first set of credentials and any further refresh hands over the creds. So creds getting creds :o. Again what to do when these creds expire and repeat... You shouldn't have to do anythin I just felt like mentioning it. The tool will check if things are valid and if not it'll build the sso file temporarily and do it that way handing back to creds. 

## Windows based powershell
from powershell these varaibles to your power and make sure they're loaded in your environment (ensure you remove any previous `GO` env vars)
## Setting up the environment
```bash
$env:AWS_PROFILE = "<org>-master-<env>-<role>"
$env:AWS_DEFAULT_REGION = "eu-west-2"
$env:PATH += ";$HOME\go\bin"
$env:GOPRIVATE = "github.com/<org>/override"
$env:GONOPROXY = "github.com/<org>/override"
```
## Installation notes
To authenticate to github in order to install the tool, create a `$HOME/.netrc` file with this content
```
machine github.com login <handle>_SLCGOVUK password <TOKEN>
```
You can get a token for this file via `gh auth token` (you have to be logged via the tool to get a token)

## Install the app
```bash
go install github.com/<org>/override@v1.7.0-rc.8
```
test with 
```bash
override version
```
# Usage
Use `override help`

Overrides create a `~/.override` config file on first run with sane defaults:

```json
// linux
{
 "overrides_provider_file": "overrides.tf",
 "chunks": 4,
 "threads": 12,
 "verbose": false,
 "refresh": false,
 "tmp_dir": "/tmp",
 "use_credentials_file": false,
 "aws_sso_cache_dir": "/home/ec2-user/.aws/sso/cache",
 "aws_credentials_file": "/home/ec2-user/.aws/credentials",
 "aws_sso_profiles_config": "/home/ec2-user/.aws/config",
 "aws_sso_profile_name": "<org>-master-<env>-<role>",
 "aws_sso_profile_account_id": "<accountid>",
 "aws_sso_profile_role": "<role>",
 "aws_region": "eu-west-2",
 "aws_sso_start_url": "https://<org>.awsapps.com/start",
 "reset_to_default_aws_sso_config_file": false

}
```
```json
// windows
{
 "overrides_provider_file": "overrides.tf",
 "chunks": 4,
 "threads": 12,
 "verbose": false,
 "refresh": false,
 "tmp_dir": "C:\\Users\\<user>\\AppData\\Local\\Temp",
 "use_credentials_file": true,
 "aws_sso_cache_dir": "C:\\Users\\<user>\\.aws\\sso\\cache",
 "aws_credentials_file": "C:\\Users\\<user>\\.aws\\credentials",
 "aws_sso_profiles_config": "C:\\Users\\<user>\\.aws\\config",
 "aws_sso_profile_name": "<org>-master-<env>-<role>",
 "aws_sso_profile_account_id": "<accountid>",
 "aws_sso_profile_role": "<role>",
 "aws_region": "eu-west-2",
 "aws_sso_start_url": "https://<org>loginsso.awsapps.com/start",
 "reset_to_default_aws_sso_config_file": false
}
```

All of these config options can be set using `override config -set <option> <value>` is this equivalent to running `override <subcommand> --<options>` for example `override config -set verbose true` is equivalent to running `override apply --verbose`

| config options      | description      |
| ------------- | ------------- |
| overrides_provider_file | the provider file that's written to disk when you run `override apply` |
| chunks | the number of account fetched at a time when during a refresh. This is a tunable only altered when you hit rate limiting responses from aws api, which is agressively rate limited |
| threads | the number of threads started at a runtime when during a refresh. This is a tunable only altered when you hit rate limiting responses from aws api, which is agressively rate limited |
| verbose | enable verbose logging |
| refresh | enable refreshes on every run |
| tmp_dir | the tmp dir of the system where hcl translation takes place, can be overwritten if you have permissions issues |
| use_credentials_file | decides how terraform will authenticate to aws at `terraform plan` i.e. whether to use the credentials file, off be default as sso is preferred but if not using sso you can turn this on |
| aws_sso_cache_dir | default location of aws sso cache dir, can be overwtitten if you have permissions issues |
| aws_credentials_file | default location of the aws credentials file, can be overwtitten if you have permissions issues, defaults to true on windows |
| aws_sso_profiles_config | default location of the aws sso file, can be overwtitten if you have permissions issues |
| aws_sso_profile_name | the default sso profile name to use when the `-reset-to-default-aws-sso-config` flag is used |
| aws_sso_profile_account_id |  the default sso account id name to use when the `-reset-to-default-aws-sso-config` flag is used |
| aws_sso_profile_role |  the default sso role name to use when the `-reset-to-default-aws-config` flag is used |
| aws_region | the default sso region name to use when the `-reset-to-default-aws-sso-config` flag is used |
| aws_sso_start_url |  the default sso start url name to use when the `-reset-to-default-aws-sso-config` flag is used |
| reset_to_default_aws_sso_config_file | whether the `~/.aws/config` file should be reset on every run |

# common issues
```
2025/02/14 14:59:35 open /home/ec2-user/.aws/sso/cache: no such file or directory
```
ensure that you're logged in with sso before running the tool, aws cli will create this directory


# Rate limits
Roles are fetched using next tokens per account. When the tool is too fast aws returns a `TooManyRequestsException` in this case requests are incrementally backked off and retried, reguardless of the retry log, the api will only response to a maximum of 3 attempts within the grace period. If you exceed this number of attempts the following two error messages will be returned: 
```
operation error SSO: ListAccountRoles, failed to get rate limit token, retry quota exceeded, 4 available, 5 requested
```
and
```
operation error SSO: ListAccountRoles, exceeded maximum number of attempts, 3, https response error StatusCode: 429, RequestID: 702b0153-c44e-4a6f-aad6-e661a876996f, TooManyRequestsException: HTTP 429 Unknown Code
```
The more account we add and the more roles per account we add the more requests the tool will have to make. The above messages are telling us that the request limit for the ListAccountRoles api is 4 per second but 5 were requested, and that 3 attempts of the same request exceed the rate limit.

The `--batchsize` allows you to adjust how roles are grouped and processed into blocks as the number of accounts increases. The `--workers` tunable allows you to pass how many workers should be used to processed these chunks.
# Further Config
This tool's default behaviour can be extended with a `mapping.hcl` file. This file is read in and used to build the `overrides.tf` file when the default behaviour is not enough. There are 2 `keywords` in this file that the tool is aware of `default` and `unaliased`. Since overrides job is to build new providers file with credentials that the local user has access to, when using cross account providers this requires the `mappings.hcl` file. The `default` keyword applies to all providers hence, "default" when you specific this in the mappings file it replaces all credentials for all *aliased* providers with this mapping.
```
override default {
    profile = "<org>-<Account>-<Environment>-<Role>
}
``` 
to do the same thing for an unalised provider, `override apply` also has a tertiery option for allows you to override the environment of this provider `override apply --alias dev`
```
override unaliased {
    profile = "<org>-<Account>-<Environment>-<Role>
}
``` 
For special cases where you want to change the behaviour of a specific provider, you specify the override by provider alias name 
 ```
override <org>-my-alias-A {
    profile = "<org>-<AccountA>-<Environment>-<Role>
}
override <org>-my-alias-B {
    profile = "<org>-<AccountB>-<Environment>-<Role>
}
```
`default` if mean to cover 99% of your cases where the other functions can be used to tune your providers file per code base.
