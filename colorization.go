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
	{
		color.New(color.FgHiBlue).Add(color.Bold),
		color.New(color.FgBlue).Add(color.Bold),
	},
	{
		color.New(color.FgHiCyan).Add(color.Bold),
		color.New(color.FgCyan).Add(color.Bold),
	},
	{
		color.New(color.FgHiGreen).Add(color.Bold),
		color.New(color.FgGreen).Add(color.Bold),
	},
	{
		color.New(color.FgHiMagenta).Add(color.Bold),
		color.New(color.FgMagenta).Add(color.Bold),
	},
	{
		color.New(color.FgHiRed).Add(color.Bold),
		color.New(color.FgRed).Add(color.Bold),
	},
	{
		color.New(color.FgHiYellow).Add(color.Bold),
		color.New(color.FgYellow).Add(color.Bold),
	},
}

func getColorConfig(parts ...string) colorConfig {
	hash := fnv.New32()
	for _, a := range parts {
		_, _ = hash.Write([]byte(a))
	}
	return colorConfigs[hash.Sum32()%uint32(len(colorConfigs))]
}
