package main

import (
	"github.com/docker/go-plugins-helpers/sdk"
)

func main() {
	h := sdk.NewHandler(`{"Implements": ["LoggingDriver"]}`)
	handlers(&h, newDriver())
	if err := h.ServeUnix("logger", 0); err != nil {
		panic(err)
	}
}
