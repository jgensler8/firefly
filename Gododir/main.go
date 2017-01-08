package main

import (
  do "gopkg.in/godo.v2"
)

func tasks(p *do.Project) {
    defaultGoos := "darwin"
    defaultGoarch := "amd64"
    defaultSource := "**/*.go"

    p.Task("default", do.S{"lint", "test", "build"}, nil)

    p.Task("lint", do.S{}, func(c *do.Context) {
      c.Run("golint ./...")
    }).Src(defaultSource)

    p.Task("test", do.S{}, func(c *do.Context) {
      // I think there is bug in their code where we won't keep watching unless
      // the return code is 0
      c.Start("go test ./...")
    }).Src(defaultSource)

    p.Task("build", do.S{}, func(c *do.Context) {
      goos := c.Args.MayString(defaultGoos, "goos")
      goarch := c.Args.MayString(defaultGoarch, "goarch")

      c.Run(`GOOS={{.goos}} GOARCH={{.goarch}} go build`, do.M{"goos": goos, "goarch": goarch})
    }).Src(defaultSource).Debounce(1000)
}

func main() {
    do.Godo(tasks)
}
