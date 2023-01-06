# carrot
[![GitHub](https://img.shields.io/github/license/mashape/apistatus.svg)](https://github.com/jiansoft/carrot)
[![Go Report Card](https://goreportcard.com/badge/github.com/jiansoft/carrot)](https://goreportcard.com/report/github.com/jiansoft/carrot)
[![build-test](https://github.com/jiansoft/carrot/actions/workflows/go.yml/badge.svg)](https://github.com/jiansoft/carrot/actions/workflows/go.yml)
[![](https://img.shields.io/github/tag/jiansoft/carrot.svg)](https://github.com/jiansoft/carrot/releases)
### Features

Memory Cache
-------
Implements an in-memory cache key:value (similar to C# MemoryCache)

Usage
================

### Quick Start

1.Install
~~~
  go get github.com/jiansoft/carrot
~~~

2.Use examples
~~~ golang
import (
    "time"
    
    "github.com/jiansoft/carrot"
)

func main() {
    
    //Keep an item in memory 
    carrot.Default.KeepDelay("qq", "Qoo", time.Second)
    //Read returns the value if the key exists in the cache and it's not expired.
    val, ok := carrot.Default.Read("qq")
    //Have eturns true if the memory has the item and it's not expired.
    yes := carrot.Default.Have("qq")
    //Removes an item from the memory
    carrot.Default.Forget("qq")

~~~
[More example](<https://github.com/jiansoft/carrot/blob/main/exemple/main.go>)

## License

Copyright (c) 2023

Released under the MIT license:

- [www.opensource.org/licenses/MIT](http://www.opensource.org/licenses/MIT)
