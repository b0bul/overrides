locals {
  repository_name = "EXAMPLEREPONAME"
  account_no = "123456789010112"
  some_list       = [1, 2, 3, 4, 5]
  some_map = {
    hello_from_the_locals_file = "yolo"
  }
  some_bool = false
  some_set  = {}
  some_list_of_maps = [{
    key1 : "val1"
    }, {
    key2 : "val2"
  }]
  default_tags = {
    Respository = "yolo123"
  }
}

locals {
    secondary = "secondary"
}

