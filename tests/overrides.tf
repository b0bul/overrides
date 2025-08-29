provider "aws" {
	region = "eu-west-2"
	profile = "org-account-env-role"
	shared_credentials_files = ["C:\Users\<user>\.aws\credentials"]
	shared_config_files = ["C:\Users\<user>\.aws\config"]
 default_tags {
   tags = {
    Repository   =   ""
       }
    }
}
provider "aws" {
	region = "eu-west-2"
	alias = "yolo"
	profile = "org-account-env-role"
	shared_credentials_files = ["C:\Users\<user>\.aws\credentials"]
	shared_config_files = ["C:\Users\<user>\.aws\config"]
 default_tags {
   tags = {
    Repository   =   ""
       }
    }
}
