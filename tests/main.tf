locals {
  logs_account_id = "123456789101112"
  some_list       = []
  some_map = {
    key : "value"
  }
  some_bool = true
  some_set  = {}
  some_list_of_maps = [{
    key1 : "value"
    }, {
    key2 : "value"
  }]
}

module "test" {}

resource aws_s3_bukcet "test" {}
