// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"time"

	"github.com/mitchellh/go-homedir"
)

// DefaultLogDirectory ...
const DefaultLogDirectory = "~/.gecko/logs"

// Config ...
type Config struct {
	RotationInterval                                                                                time.Duration
	FileSize, RotationSize, FlushSize                                                               int
	DisableLogging, DisableDisplaying, DisableContextualDisplaying, DisableFlushOnWrite, Assertions bool
	LogLevel, DisplayLevel                                                                          Level
	Directory, MsgPrefix                                                                            string
}

// DefaultConfig ...
func DefaultConfig() (Config, error) {
	dir, err := homedir.Expand(DefaultLogDirectory)
	return Config{
		RotationInterval: 24 * time.Hour,
		FileSize:         1 << 23, // 8 MB
		RotationSize:     7,
		FlushSize:        1,
		DisplayLevel:     Info,
		LogLevel:         Debug,
		Directory:        dir,
	}, err
}