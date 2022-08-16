package main

import (
	"hash/fnv"

	"github.com/fatih/color"
)

type colorConfig struct {
	labels   *color.Color
	metadata *color.Color
}

var colorConfigs = []colorConfig{
	{color.New(color.FgHiBlue), color.New(color.FgBlue)},
	{color.New(color.FgHiCyan), color.New(color.FgCyan)},
	{color.New(color.FgHiGreen), color.New(color.FgGreen)},
	{color.New(color.FgHiMagenta), color.New(color.FgMagenta)},
	{color.New(color.FgHiRed), color.New(color.FgRed)},
	{color.New(color.FgHiYellow), color.New(color.FgYellow)},
}

func getColorConfig(parts ...string) colorConfig {
	hash := fnv.New32()
	for _, a := range parts {
		_, _ = hash.Write([]byte(a))
	}
	return colorConfigs[hash.Sum32()%uint32(len(colorConfigs))]
}
